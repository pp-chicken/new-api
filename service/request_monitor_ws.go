package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type monitorParentKey struct{}

type MonitorSocket struct {
	MessagesIn   int64     `json:"messages_in"`
	MessagesOut  int64     `json:"messages_out"`
	BytesIn      int64     `json:"bytes_in"`
	BytesOut     int64     `json:"bytes_out"`
	LastActivity time.Time `json:"last_activity"`
	CloseCode    int       `json:"close_code"`
	Error        string    `json:"error"`
	ErrorDetail  string    `json:"error_detail,omitempty"`
}

type MonitorWebSocket struct {
	Client           MonitorSocket `json:"client"`
	Upstream         MonitorSocket `json:"upstream"`
	CloseSource      string        `json:"close_source"`
	GatewayCloseCode int           `json:"gateway_close_code"`
}

// StartMonitorWSCall 将单次生成与长连接拆开，保留父连接供握手和消息统计关联。
func StartMonitorWSCall(c *gin.Context) {
	parent, _ := c.Request.Context().Value(monitorContextKey{}).(*requestMonitorTrace)
	ctx := context.WithValue(c.Request.Context(), monitorParentKey{}, parent)
	ctx = context.WithValue(ctx, monitorContextKey{}, (*requestMonitorTrace)(nil))
	c.Request = c.Request.WithContext(ctx)
	StartRequestMonitor(c)
	trace, _ := c.Request.Context().Value(monitorContextKey{}).(*requestMonitorTrace)
	trace.mu.Lock()
	trace.data.Kind = "ws_request"
	if parent != nil {
		trace.data.ParentID = parent.data.ID
	}
	trace.mu.Unlock()
}

func MonitorWSConnected(c *gin.Context) {
	trace, _ := c.Request.Context().Value(monitorContextKey{}).(*requestMonitorTrace)
	if trace == nil {
		return
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.data.Kind = "websocket"
	trace.data.Status = http.StatusSwitchingProtocols
	trace.data.UserID = c.GetInt("id")
	trace.data.WebSocket = &MonitorWebSocket{}
}

// MonitorWSMessage 只统计成功收发的应用消息，不保存消息体或对端自由文本。
func MonitorWSMessage(ctx context.Context, side string, incoming bool, size int, err error) {
	trace, _ := ctx.Value(monitorContextKey{}).(*requestMonitorTrace)
	if trace == nil {
		return
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	ws := trace.data.WebSocket
	if ws == nil {
		return
	}
	socket := &ws.Client
	if side == "upstream" {
		socket = &ws.Upstream
	}
	socket.LastActivity = time.Now()
	if err == nil {
		if incoming {
			socket.MessagesIn++
			socket.BytesIn += int64(size)
		} else {
			socket.MessagesOut++
			socket.BytesOut += int64(size)
		}
		return
	}
	if ws.GatewayCloseCode != 0 || socket.Error != "" || socket.CloseCode != 0 {
		return
	}
	socket.ErrorDetail = monitorErrorDetail(err)
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		socket.CloseCode = closeErr.Code
		if closeErr.Code != websocket.CloseNormalClosure && closeErr.Code != websocket.CloseGoingAway {
			socket.Error = "websocket_close"
		}
	} else {
		socket.Error = monitorError(err)
	}
	// 本地关闭后的 read 错误不能反过来被解释为对端先断开。
	if ws.CloseSource == "" {
		ws.CloseSource = side
	}
}

func MonitorWSShutdown(ctx context.Context, code int) {
	trace, _ := ctx.Value(monitorContextKey{}).(*requestMonitorTrace)
	if trace == nil {
		return
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if trace.data.WebSocket == nil {
		return
	}
	ws := trace.data.WebSocket
	if ws.CloseSource == "" {
		ws.CloseSource = "gateway"
	}
	ws.GatewayCloseCode = code
}

func MonitorWSHandshake(c *gin.Context, endpoint string, started time.Time, response *http.Response, err error) {
	trace, _ := c.Request.Context().Value(monitorContextKey{}).(*requestMonitorTrace)
	parent, _ := c.Request.Context().Value(monitorParentKey{}).(*requestMonitorTrace)
	parsed, parseErr := url.Parse(endpoint)
	if parseErr != nil {
		return
	}
	row := MonitoredUpstream{ChannelID: c.GetInt("channel_id"), Host: parsed.Host, Method: "GET", StartedAt: started, HeadersMS: time.Since(started).Milliseconds(), Protocol: "WebSocket", End: "handshake_complete"}
	if response != nil {
		row.Status = response.StatusCode
		row.RequestID = response.Header.Get(common.RequestIdKey)
		if row.RequestID == "" {
			row.RequestID = response.Header.Get("X-Request-Id")
		}
		row.RequestID = row.RequestID[:min(len(row.RequestID), 256)]
	}
	row.DurationMS = row.HeadersMS
	if err != nil {
		row.End = monitorError(err)
		row.ErrorDetail = monitorErrorDetail(err)
	}
	for _, target := range []*requestMonitorTrace{trace, parent} {
		if target == nil {
			continue
		}
		target.mu.Lock()
		target.data.ChannelID = row.ChannelID
		target.data.Model = common.GetContextKeyString(c, constant.ContextKeyOriginalModel)
		target.data.Model = target.data.Model[:min(len(target.data.Model), 256)]
		if len(target.data.Upstreams) < maxMonitorUpstreams {
			target.data.Upstreams = append(target.data.Upstreams, row)
		} else {
			target.data.Truncated = true
		}
		target.mu.Unlock()
	}
}

// UpdateMonitorRequestIdentity 在路由完成时更新快照，长时间生成时也可查看模型和用户。
func UpdateMonitorRequestIdentity(c *gin.Context) {
	trace, _ := c.Request.Context().Value(monitorContextKey{}).(*requestMonitorTrace)
	if trace == nil {
		return
	}
	events := RequestPolicy(c).Events()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.data.UserID, trace.data.ChannelID = c.GetInt("id"), c.GetInt("channel_id")
	model := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)
	trace.data.Model = model[:min(len(model), 256)]
	if len(events) > 128 {
		events = events[len(events)-128:]
		trace.data.Truncated = true
	}
	trace.data.Events = events
}
