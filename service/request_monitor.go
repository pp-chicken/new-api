package service

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
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
	ErrorDetail string    `json:"error_detail,omitempty"`
}

type MonitoredRequest struct {
	Kind              string                        `json:"kind"`
	ParentID          string                        `json:"parent_id"`
	WebSocket         *MonitorWebSocket             `json:"websocket,omitempty"`
	ID                string                        `json:"id"`
	EndedAt           *time.Time                    `json:"ended_at,omitempty"`
	StartedAt         time.Time                     `json:"started_at"`
	ExpiresAt         time.Time                     `json:"expires_at"`
	DurationMS        int64                         `json:"duration_ms"`
	Method            string                        `json:"method"`
	Path              string                        `json:"path"`
	Model             string                        `json:"model"`
	UserID            int                           `json:"user_id"`
	Username          string                        `json:"username,omitempty"`
	TokenID           int                           `json:"token_id,omitempty"`
	TokenName         string                        `json:"token_name,omitempty"`
	ChannelID         int                           `json:"channel_id"`
	Status            int                           `json:"status"`
	Bytes             int                           `json:"bytes"`
	State             string                        `json:"state"`
	ClientError       string                        `json:"client_error"`
	ClientErrorDetail string                        `json:"client_error_detail,omitempty"`
	StreamEnd         string                        `json:"stream_end"`
	StreamOutcome     string                        `json:"stream_outcome"`
	StreamError       string                        `json:"stream_error,omitempty"`
	StreamDetails     *relaycommon.StreamDiagnostic `json:"stream_details,omitempty"`
	Upstreams         []MonitoredUpstream           `json:"upstreams"`
	Events            []PolicyEvent                 `json:"events"`
	Truncated         bool                          `json:"truncated"`
	Bodies            []MonitoredBody               `json:"bodies,omitempty"`
	BodiesUnavailable string                        `json:"bodies_unavailable,omitempty"`
}

type requestMonitorTrace struct {
	mu           sync.Mutex
	data         MonitoredRequest
	streamFailed bool
	bodies       *monitorBodyRecord
}

type requestMonitorStore struct {
	mu   sync.Mutex
	rows [RequestMonitorCapacity]*requestMonitorTrace
	next int
}

var requestMonitor requestMonitorStore
var RequestMonitorInstance = common.NewRequestId()

// captureIdentity 只取已解析的身份元数据，不复制 Token 密钥或认证头。
func (d *MonitoredRequest) captureIdentity(c *gin.Context) {
	d.UserID = c.GetInt("id")
	d.Username = c.GetString("username")
	d.TokenID = c.GetInt("token_id")
	d.TokenName = c.GetString("token_name")
	for _, value := range []*string{&d.Username, &d.TokenName} {
		if len(*value) > 256 {
			end := 256
			for !utf8.RuneStart((*value)[end]) {
				end--
			}
			*value = (*value)[:end]
			d.Truncated = true
		}
	}
}

var startMonitorCleanup = sync.OnceFunc(func() {
	go func() {
		for now := range time.Tick(time.Minute) {
			requestMonitor.expire(now)
		}
	}()
})

func (s *requestMonitorStore) expire(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, trace := range s.rows {
		if trace != nil && !now.Before(trace.data.ExpiresAt) {
			trace.bodies.release("expired")
			s.rows[index] = nil
		}
	}
}

// StartRequestMonitor 旁路采集正文，认证头和查询参数不进入诊断记录。
func StartRequestMonitor(c *gin.Context) {
	if trace, _ := c.Request.Context().Value(monitorContextKey{}).(*requestMonitorTrace); trace != nil {
		return
	}
	now := time.Now()
	trace := &requestMonitorTrace{data: MonitoredRequest{
		ID: c.GetString(common.RequestIdKey), StartedAt: now, ExpiresAt: now.Add(RequestMonitorRetention),
		Kind: "http", Method: c.Request.Method, Path: c.FullPath(), State: "running",
		Upstreams: []MonitoredUpstream{}, Events: []PolicyEvent{},
	}}
	if trace.data.ID == "" {
		trace.data.ID = common.NewRequestId()
	}
	trace.data.captureIdentity(c)
	trace.bodies = requestMonitorBodies.start(trace.data.ExpiresAt)
	if c.Request.Body != nil {
		c.Request.Body = &monitorRequestBody{ReadCloser: c.Request.Body, record: trace.bodies, direction: "request", contentType: c.GetHeader("Content-Type")}
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), monitorContextKey{}, trace))
	requestMonitor.mu.Lock()
	if previous := requestMonitor.rows[requestMonitor.next]; previous != nil {
		previous.bodies.release("evicted")
	}
	requestMonitor.rows[requestMonitor.next] = trace
	requestMonitor.next = (requestMonitor.next + 1) % RequestMonitorCapacity
	requestMonitor.mu.Unlock()
	startMonitorCleanup()
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
	d.captureIdentity(c)
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
	d.ChannelID = c.GetInt("channel_id")
	d.Status, d.Bytes = c.Writer.Status(), max(0, c.Writer.Size())
	d.State = "completed"
	d.ClientError = monitorError(writeErr)
	d.ClientErrorDetail = monitorErrorDetail(writeErr)
	if err := c.Request.Context().Err(); err != nil {
		d.ClientError = monitorError(err)
		d.ClientErrorDetail = monitorErrorDetail(err)
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
	trace.data.StreamError = monitorErrorDetail(stream.EndError)
	detail := stream.DiagnosticSnapshot()
	for _, value := range []*string{&detail.Event, &detail.ResponseID, &detail.ErrorCode, &detail.ErrorType, &detail.ErrorMessage, &detail.ErrorParam, &detail.IncompleteReason} {
		cleaned, truncated := monitorDiagnosticText(*value, common.GetContextKeyString(c, constant.ContextKeyChannelKey), c.GetHeader("Authorization"))
		*value = cleaned
		detail.Truncated = detail.Truncated || truncated
	}
	trace.data.StreamDetails = nil
	if detail != (relaycommon.StreamDiagnostic{}) {
		trace.data.StreamDetails = &detail
	}
}

func (trace *requestMonitorTrace) snapshot() MonitoredRequest {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	data := trace.data
	if data.StreamDetails != nil {
		detail := *data.StreamDetails
		data.StreamDetails = &detail
	}
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
	requestMonitor.expire(time.Now())
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

// RequestMonitorDetail 仅详情接口读取正文，列表轮询不复制或传输正文。
func RequestMonitorDetail(id string) (MonitoredRequest, bool) {
	requestMonitor.expire(time.Now())
	requestMonitor.mu.Lock()
	var trace *requestMonitorTrace
	for _, candidate := range requestMonitor.rows {
		if candidate != nil && candidate.data.ID == id {
			trace = candidate
			break
		}
	}
	requestMonitor.mu.Unlock()
	if trace == nil {
		return MonitoredRequest{}, false
	}
	row := trace.snapshot()
	row.Bodies, row.BodiesUnavailable = trace.bodies.snapshot(time.Now())
	if row.BodiesUnavailable == "expired" {
		return MonitoredRequest{}, false
	}
	return row, true
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

// monitorErrorDetail 从错误类型重建诊断说明，不保留包装错误中的 URL、
// 认证信息或对端自由文本。未知错误仍使用固定说明，避免将正文存入监看。
func monitorErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		detail := &websocket.CloseError{Code: closeErr.Code}
		// Gorilla 在连接未收到关闭帧就结束时生成这个固定错误。
		if closeErr.Code == websocket.CloseAbnormalClosure && closeErr.Text == io.ErrUnexpectedEOF.Error() {
			detail.Text = io.ErrUnexpectedEOF.Error()
		}
		return detail.Error()
	}
	for _, known := range []error{
		context.Canceled, context.DeadlineExceeded, io.ErrUnexpectedEOF, io.EOF,
		net.ErrClosed, syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.EPIPE,
		websocket.ErrReadLimit, websocket.ErrBadHandshake, websocket.ErrCloseSent,
		bufio.ErrTooLong,
	} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		switch {
		case dnsErr.IsTimeout:
			return "DNS lookup timed out"
		case dnsErr.IsNotFound:
			return "DNS host not found"
		default:
			return "DNS lookup failed"
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "network I/O timeout"
	}
	return "transport error (details omitted)"
}

var monitorCredentialPattern = regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[^\s"',;]+|\bsk-[a-z0-9_-]+|(?:["']?(?:api[_-]?key|access[_-]?token|refresh[_-]?token|authorization|password|secret)["']?\s*[:=]\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;}]+)`)

// monitorDiagnosticText 只处理协议错误字段，不保存完整响应、输出或请求正文。
func monitorDiagnosticText(value string, secrets ...string) (string, bool) {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	value = monitorCredentialPattern.ReplaceAllString(value, "[redacted]")
	value = common.MaskSensitiveInfo(value)
	const maxBytes = 4096
	if len(value) <= maxBytes {
		return value, false
	}
	end := maxBytes
	for !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + "…", true
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
	trace.data.StreamEnd, trace.data.StreamOutcome, trace.data.StreamError = "", "", ""
	trace.data.StreamDetails = nil
	trace.data.captureIdentity(c)
	trace.data.ChannelID = c.GetInt("channel_id")
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
	// 独立克隆请求，只在底层 transport 读取时复制正文，GetBody 重放能力保持不变。
	req = req.Clone(req.Context())
	if req.Body != nil {
		req.Body = &monitorRequestBody{ReadCloser: req.Body, record: t.trace.bodies, direction: "upstream_request", attempt: index + 1, channelID: t.channelID, contentType: req.Header.Get("Content-Type")}
	}
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
		row.ErrorDetail = monitorErrorDetail(err)
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
			resp.Body = &monitoredBody{ReadCloser: resp.Body, trace: t.trace, index: index, started: started, channelID: t.channelID, contentType: resp.Header.Get("Content-Type")}
		}
	}
	t.trace.mu.Unlock()
	return resp, err
}

type monitoredBody struct {
	io.ReadCloser
	trace       *requestMonitorTrace
	index       int
	started     time.Time
	contentType string
	channelID   int
}

func (b *monitoredBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.trace.bodies.append(time.Now(), "upstream_response", b.index+1, b.channelID, b.contentType, p[:n])
	b.trace.mu.Lock()
	row := &b.trace.data.Upstreams[b.index]
	row.Bytes += int64(n)
	row.DurationMS = time.Since(b.started).Milliseconds()
	if err != nil {
		row.End = monitorError(err)
		if !errors.Is(err, io.EOF) {
			row.ErrorDetail = monitorErrorDetail(err)
		}
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
		row.ErrorDetail = monitorErrorDetail(err)
	}
	b.trace.mu.Unlock()
	return err
}
