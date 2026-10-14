package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const RequestMonitorCapacity = 500
const maxMonitorUpstreams = 64

type monitorContextKey struct{}

type MonitoredUpstream struct {
	ChannelID   int       `json:"channel_id"`
	Host        string    `json:"host"`
	Method      string    `json:"method"`
	StartedAt   time.Time `json:"started_at"`
	HeadersMS   int64     `json:"headers_ms"`
	FirstByteMS int64     `json:"first_byte_ms"`
	DurationMS  int64     `json:"duration_ms"`
	Status      int       `json:"status"`
	Protocol    string    `json:"protocol"`
	RequestID   string    `json:"request_id"`
	Reused      bool      `json:"reused"`
	Bytes       int64     `json:"bytes"`
	End         string    `json:"end"`
}

type MonitoredRequest struct {
	Kind          string              `json:"kind"`
	ParentID      string              `json:"parent_id"`
	WebSocket     *MonitorWebSocket   `json:"websocket,omitempty"`
	ID            string              `json:"id"`
	EndedAt       *time.Time          `json:"ended_at,omitempty"`
	StartedAt     time.Time           `json:"started_at"`
	DurationMS    int64               `json:"duration_ms"`
	Method        string              `json:"method"`
	Path          string              `json:"path"`
	Model         string              `json:"model"`
	UserID        int                 `json:"user_id"`
	ChannelID     int                 `json:"channel_id"`
	Status        int                 `json:"status"`
	Bytes         int                 `json:"bytes"`
	State         string              `json:"state"`
	ClientError   string              `json:"client_error"`
	StreamEnd     string              `json:"stream_end"`
	StreamOutcome string              `json:"stream_outcome"`
	Upstreams     []MonitoredUpstream `json:"upstreams"`
	Events        []PolicyEvent       `json:"events"`
	Truncated     bool                `json:"truncated"`
}

type requestMonitorTrace struct {
	mu           sync.Mutex
	data         MonitoredRequest
	streamFailed bool
}

type requestMonitorStore struct {
	mu   sync.Mutex
	rows [RequestMonitorCapacity]*requestMonitorTrace
	next int
}

var requestMonitor requestMonitorStore
var RequestMonitorInstance = common.NewRequestId()

// StartRequestMonitor 只保存诊断元数据，不读取正文、查询参数或认证头。
func StartRequestMonitor(c *gin.Context) {
	if trace, _ := c.Request.Context().Value(monitorContextKey{}).(*requestMonitorTrace); trace != nil {
		return
	}
	trace := &requestMonitorTrace{data: MonitoredRequest{
		ID: c.GetString(common.RequestIdKey), StartedAt: time.Now(),
		Kind: "http", Method: c.Request.Method, Path: c.FullPath(), State: "running",
		Upstreams: []MonitoredUpstream{}, Events: []PolicyEvent{},
	}}
	if trace.data.ID == "" {
		trace.data.ID = common.NewRequestId()
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), monitorContextKey{}, trace))
	requestMonitor.mu.Lock()
	requestMonitor.rows[requestMonitor.next] = trace
	requestMonitor.next = (requestMonitor.next + 1) % RequestMonitorCapacity
	requestMonitor.mu.Unlock()
}

func FinishRequestMonitor(c *gin.Context, writeErr error, panicked bool) {
	trace, _ := c.Request.Context().Value(monitorContextKey{}).(*requestMonitorTrace)
	if trace == nil {
		return
	}
	events := RequestPolicy(c).Events()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	d := &trace.data
	ended := time.Now()
	d.EndedAt = &ended
	d.DurationMS = time.Since(d.StartedAt).Milliseconds()
	if d.Kind == "websocket" {
		d.DurationMS = time.Since(d.StartedAt).Milliseconds()
		d.State = "completed"
		if panicked || (d.WebSocket != nil && (d.WebSocket.GatewayCloseCode != 1000 && d.WebSocket.GatewayCloseCode != 1001 || d.WebSocket.Client.Error != "" || d.WebSocket.Upstream.Error != "")) {
			d.State = "failed"
		}
		return
	}
	d.Model = common.GetContextKeyString(c, constant.ContextKeyOriginalModel)
	d.Model = d.Model[:min(len(d.Model), 256)]
	d.UserID, d.ChannelID = c.GetInt("id"), c.GetInt("channel_id")
	d.Status, d.Bytes = c.Writer.Status(), max(0, c.Writer.Size())
	d.State = "completed"
	d.ClientError = monitorError(writeErr)
	if err := c.Request.Context().Err(); err != nil {
		d.ClientError = monitorError(err)
	}
	if len(events) > 128 {
		d.Truncated = true
		events = events[len(events)-128:]
	}
	d.Events = events
	if d.Status >= 400 || d.ClientError != "" || panicked || trace.streamFailed {
		d.State = "failed"
	}
	if len(events) > 0 && events[len(events)-1].Decision.Action == "stop" {
		d.State = "failed"
	}
	if panicked {
		d.StreamEnd = "panic"
	}
}

func RecordMonitorStream(c *gin.Context, stream *relaycommon.StreamStatus) {
	if c == nil || stream == nil {
		return
	}
	trace, _ := c.Request.Context().Value(monitorContextKey{}).(*requestMonitorTrace)
	if trace == nil {
		return
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.streamFailed = !stream.IsNormalEnd() || stream.HasErrors() || (stream.ResponseOutcome() != "" && stream.ResponseOutcome() != "completed")
	trace.data.StreamEnd = string(stream.EndReason)
	trace.data.StreamOutcome = string(stream.ResponseOutcome())
}

func (trace *requestMonitorTrace) snapshot() MonitoredRequest {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	data := trace.data
	if data.WebSocket != nil {
		ws := *data.WebSocket
		data.WebSocket = &ws
		data.Bytes = int(ws.Client.BytesOut)
	}
	data.Upstreams = slices.Clone(data.Upstreams)
	data.Events = slices.Clone(data.Events)
	if data.State == "running" {
		data.DurationMS = time.Since(data.StartedAt).Milliseconds()
	}
	for i := range data.Upstreams {
		if data.Upstreams[i].End == "" {
			data.Upstreams[i].DurationMS = time.Since(data.Upstreams[i].StartedAt).Milliseconds()
		}
	}
	return data
}

// RequestMonitorSnapshots 按请求进入时间倒序返回当前实例的有界快照。
func RequestMonitorSnapshots() []MonitoredRequest {
	requestMonitor.mu.Lock()
	rows := make([]*requestMonitorTrace, 0, RequestMonitorCapacity)
	for i := range RequestMonitorCapacity {
		index := (requestMonitor.next - 1 - i + RequestMonitorCapacity) % RequestMonitorCapacity
		if requestMonitor.rows[index] != nil {
			rows = append(rows, requestMonitor.rows[index])
		}
	}
	requestMonitor.mu.Unlock()
	result := make([]MonitoredRequest, 0, len(rows))
	for _, trace := range rows {
		result = append(result, trace.snapshot())
	}
	return result
}

// monitorError 使用固定分类，避免底层错误字符串泄露 URL 查询参数、代理凭据和正文。
func monitorError(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected_eof"
	case errors.Is(err, io.EOF):
		return "eof"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused"
	case errors.Is(err, syscall.EPIPE):
		return "broken_pipe"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns_error"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	return "transport_error"
}

type monitoredTransport struct {
	base      http.RoundTripper
	trace     *requestMonitorTrace
	channelID int
}

// MonitorRelayTransport 包装单次转发的客户端副本，不改变共享连接池或重试策略。
func MonitorRelayTransport(c *gin.Context, base http.RoundTripper) http.RoundTripper {
	trace, _ := c.Request.Context().Value(monitorContextKey{}).(*requestMonitorTrace)
	if trace == nil {
		return base
	}
	if base == nil {
		base = http.DefaultTransport
	}
	trace.mu.Lock()
	// 总体流状态属于最新一次转发，前一次失败仍保留在上游明细和决策记录中。
	trace.streamFailed = false
	trace.data.StreamEnd, trace.data.StreamOutcome = "", ""
	trace.data.UserID, trace.data.ChannelID = c.GetInt("id"), c.GetInt("channel_id")
	model := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)
	trace.data.Model = model[:min(len(model), 256)]
	trace.mu.Unlock()
	return &monitoredTransport{base: base, trace: trace, channelID: c.GetInt("channel_id")}
}

func (t *monitoredTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.trace.mu.Lock()
	index := len(t.trace.data.Upstreams)
	if index >= maxMonitorUpstreams {
		t.trace.data.Truncated = true
		t.trace.mu.Unlock()
		return t.base.RoundTrip(req)
	}
	started := time.Now()
	t.trace.data.Upstreams = append(t.trace.data.Upstreams, MonitoredUpstream{
		ChannelID: t.channelID, Host: req.URL.Host, Method: req.Method, StartedAt: started,
	})
	t.trace.mu.Unlock()
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			t.trace.mu.Lock()
			defer t.trace.mu.Unlock()
			t.trace.data.Upstreams[index].Reused = info.Reused
		},
		GotFirstResponseByte: func() {
			t.trace.mu.Lock()
			defer t.trace.mu.Unlock()
			t.trace.data.Upstreams[index].FirstByteMS = time.Since(started).Milliseconds()
		},
	}
	resp, err := t.base.RoundTrip(req.WithContext(httptrace.WithClientTrace(req.Context(), trace)))
	t.trace.mu.Lock()
	row := &t.trace.data.Upstreams[index]
	row.HeadersMS = time.Since(started).Milliseconds()
	if err != nil {
		row.End = monitorError(err)
		row.DurationMS = row.HeadersMS
	}
	if resp != nil {
		row.Status, row.Protocol = resp.StatusCode, resp.Proto
		for _, header := range []string{common.RequestIdKey, "X-Request-Id", "Request-Id"} {
			if id := resp.Header.Get(header); id != "" {
				row.RequestID = id[:min(len(id), 256)]
				break
			}
		}
		if resp.Body != nil {
			resp.Body = &monitoredBody{ReadCloser: resp.Body, trace: t.trace, index: index, started: started}
		}
	}
	t.trace.mu.Unlock()
	return resp, err
}

type monitoredBody struct {
	io.ReadCloser
	trace   *requestMonitorTrace
	index   int
	started time.Time
}

func (b *monitoredBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.trace.mu.Lock()
	row := &b.trace.data.Upstreams[b.index]
	row.Bytes += int64(n)
	row.DurationMS = time.Since(b.started).Milliseconds()
	if err != nil {
		row.End = monitorError(err)
	}
	b.trace.mu.Unlock()
	return n, err
}

func (b *monitoredBody) Close() error {
	err := b.ReadCloser.Close()
	b.trace.mu.Lock()
	row := &b.trace.data.Upstreams[b.index]
	row.DurationMS = time.Since(b.started).Milliseconds()
	if row.End == "" {
		row.End = "closed"
	}
	if err != nil {
		row.End = monitorError(err)
	}
	b.trace.mu.Unlock()
	return err
}
