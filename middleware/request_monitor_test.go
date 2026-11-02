package middleware_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestMonitorTruncatedUpstreamPreservesHTTP200AndFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Header().Set("X-Request-Id", "upstream-123")
		_, _ = io.WriteString(w, "partial")
	}))
	defer upstream.Close()
	engine := gin.New()
	engine.Use(middleware.RequestId(), middleware.RouteTag("relay"))
	engine.POST("/v1/test", func(c *gin.Context) {
		c.Set("channel_id", 7)
		service.RequestPolicy(c).BeginAttempt(&model.Channel{Id: 7}, "default")
		request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstream.URL+"/?key=secret-query", strings.NewReader("secret-body"))
		require.NoError(t, err)
		request.Header.Set("Authorization", "Bearer secret-token")
		client := &http.Client{Transport: service.MonitorRelayTransport(c, http.DefaultTransport)}
		response, err := client.Do(request)
		require.NoError(t, err)
		body, err := io.ReadAll(response.Body)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.NoError(t, response.Body.Close())
		status := relaycommon.NewStreamStatus()
		status.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
		service.MarkRequestPolicySuccess(c, status)
		c.Data(http.StatusOK, "text/event-stream", body)
	})
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/test?token=downstream-secret", nil))
	id := response.Header().Get(common.RequestIdKey)
	rows := service.RequestMonitorSnapshots()
	require.NotEmpty(t, rows)
	row := rows[0]
	require.Equal(t, id, row.ID)
	assert.Equal(t, "failed", row.State)
	assert.Equal(t, 200, row.Status)
	assert.Equal(t, "/v1/test", row.Path)
	assert.Equal(t, "scanner_error", row.StreamEnd)
	require.Len(t, row.Upstreams, 1)
	assert.Equal(t, "unexpected_eof", row.Upstreams[0].End)
	assert.Equal(t, int64(7), row.Upstreams[0].Bytes)
	assert.Equal(t, 200, row.Upstreams[0].Status)
	assert.Equal(t, "upstream-123", row.Upstreams[0].RequestID)
	serialized, err := common.Marshal(row)
	require.NoError(t, err)
	assert.NotContains(t, string(serialized), "secret")
	var diagnostic map[string]any
	require.NoError(t, common.Unmarshal(serialized, &diagnostic))
	assert.Equal(t, "unexpected EOF", diagnostic["stream_error"])
	assert.Equal(t, "unexpected EOF", diagnostic["upstreams"].([]any)[0].(map[string]any)["error_detail"])
	// 快照是独立副本，调用方不能修改后续查询结果。
	rows[0].Upstreams[0].End = "changed"
	assert.Equal(t, "unexpected_eof", service.RequestMonitorSnapshots()[0].Upstreams[0].End)
}

func TestRequestMonitorCanceledDownstreamAndRetries(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	engine := gin.New()
	engine.Use(middleware.RequestId(), middleware.RouteTag("relay"))
	engine.GET("/retry", func(c *gin.Context) {
		for _, channel := range []int{1, 2} {
			c.Set("channel_id", channel)
			client := &http.Client{Transport: service.MonitorRelayTransport(c, http.DefaultTransport)}
			request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, upstream.URL, nil)
			require.NoError(t, err)
			response, err := client.Do(request)
			require.NoError(t, err)
			_, err = io.Copy(io.Discard, response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			if channel == 1 {
				status := relaycommon.NewStreamStatus()
				status.MarkFailed("upstream_timeout", "server_error", 503)
				status.RecordDiagnostic(relaycommon.StreamDiagnostic{Event: "error", ErrorMessage: "previous attempt failed"})
				status.SetEndReason(relaycommon.StreamEndReasonTimeout, context.DeadlineExceeded)
				service.RecordMonitorStream(c, status)
			}
		}
		c.Status(http.StatusOK)
	})
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/retry", nil))
	row := service.RequestMonitorSnapshots()[0]
	assert.Equal(t, "completed", row.State)
	require.Len(t, row.Upstreams, 2)
	assert.Equal(t, 503, row.Upstreams[0].Status)
	assert.Equal(t, 200, row.Upstreams[1].Status)
	assert.Equal(t, 2, row.Upstreams[1].ChannelID)
	serialized, err := common.Marshal(row)
	require.NoError(t, err)
	assert.NotContains(t, string(serialized), "context deadline exceeded")
	assert.NotContains(t, string(serialized), "stream_details")
	engine.GET("/cancel", func(c *gin.Context) { c.Status(http.StatusOK) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/cancel", nil).WithContext(ctx))
	row = service.RequestMonitorSnapshots()[0]
	assert.Equal(t, "failed", row.State)
	assert.Equal(t, "canceled", row.ClientError)
	serialized, err = common.Marshal(row)
	require.NoError(t, err)
	assert.Contains(t, string(serialized), `"client_error_detail":"context canceled"`)
}

func TestRequestMonitorResponseDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, event string
		want        map[string]any
	}{
		{name: "未完成原因", event: `{"type":"response.incomplete","response":{"id":"resp-limit","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`, want: map[string]any{"event": "response.incomplete", "response_id": "resp-limit", "incomplete_reason": "max_output_tokens"}},
		{name: "缺失未完成原因", event: `{"type":"response.incomplete","response":{"status":"incomplete"}}`, want: map[string]any{"event": "response.incomplete"}},
		{name: "响应失败详情", event: `{"type":"response.failed","response":{"id":"resp-failed","status":"failed","error":{"code":"server_error","type":"server_error","message":"The upstream could not finish.\nPlease retry.","param":"model"}}}`, want: map[string]any{"event": "response.failed", "response_id": "resp-failed", "error_code": "server_error", "error_type": "server_error", "error_message": "The upstream could not finish.\nPlease retry.", "error_param": "model"}},
		{name: "SSE 平铺错误", event: `{"type":"error","code":"context_length_exceeded","message":"Input too long","param":"input"}`, want: map[string]any{"event": "error", "error_code": "context_length_exceeded", "error_message": "Input too long", "error_param": "input"}},
		{name: "WebSocket 嵌套错误", event: `{"type":"error","status":429,"error":{"code":"rate_limit_exceeded","type":"rate_limit_error","message":"Too many requests","param":"model"}}`, want: map[string]any{"event": "error", "error_code": "rate_limit_exceeded", "error_type": "rate_limit_error", "error_status": float64(429), "error_message": "Too many requests", "error_param": "model"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(middleware.RequestId(), middleware.RouteTag("relay"))
			engine.POST("/v1/responses", func(c *gin.Context) {
				status := relaycommon.NewStreamStatus()
				info := &relaycommon.RelayInfo{StreamStatus: status}
				var event dto.ResponsesStreamResponse
				require.NoError(t, common.UnmarshalJsonStr(tc.event, &event))
				service.NewResponsesUsageAccumulator(info).Observe(&event, []byte(tc.event))
				status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				service.MarkRequestPolicySuccess(c, status)
			})
			engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
			row := service.RequestMonitorSnapshots()[0]
			assert.Equal(t, "failed", row.State)
			assert.Equal(t, "done", row.StreamEnd)
			serialized, err := common.Marshal(row)
			require.NoError(t, err)
			var diagnostic map[string]any
			require.NoError(t, common.Unmarshal(serialized, &diagnostic))
			assert.Equal(t, tc.want, diagnostic["stream_details"])
			require.NotNil(t, row.StreamDetails)
			row.StreamDetails.ErrorMessage = "changed snapshot"
			assert.NotEqual(t, "changed snapshot", service.RequestMonitorSnapshots()[0].StreamDetails.ErrorMessage)
		})
	}
}

func TestRequestMonitorResponseDiagnosticRedactionAndLimits(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		truncated     bool
	}{
		{name: "凭据脱敏", message: `Invalid key plain-channel-credential; Bearer secret-bearer; sk-secret-key; {"api_key":"secret-api-key"}; https://secret-user:secret-password@example.test/error?token=secret-query`},
		{name: "中文消息限长", message: strings.Repeat("错误详情", 1200), truncated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(middleware.RequestId(), middleware.RouteTag("relay"))
			engine.POST("/v1/responses", func(c *gin.Context) {
				payload, err := common.Marshal(map[string]any{
					"type": "response.failed",
					"response": map[string]any{
						"status": "failed",
						"error":  map[string]any{"code": "server_error", "message": tc.message},
						"output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "private-output-body"}}}},
					},
				})
				require.NoError(t, err)
				var event dto.ResponsesStreamResponse
				require.NoError(t, common.Unmarshal(payload, &event))
				info := &relaycommon.RelayInfo{
					StreamStatus: relaycommon.NewStreamStatus(),
					ChannelMeta:  &relaycommon.ChannelMeta{ApiKey: "plain-channel-credential"},
				}
				service.NewResponsesUsageAccumulator(info).Observe(&event, payload)
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				service.MarkRequestPolicySuccess(c, info.StreamStatus)
			})
			engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
			row := service.RequestMonitorSnapshots()[0]
			require.NotNil(t, row.StreamDetails)
			assert.Equal(t, tc.truncated, row.StreamDetails.Truncated)
			assert.True(t, utf8.ValidString(row.StreamDetails.ErrorMessage))
			assert.LessOrEqual(t, len(row.StreamDetails.ErrorMessage), 4096+len("…"))
			serialized, err := common.Marshal(row)
			require.NoError(t, err)
			for _, secret := range []string{"plain-channel-credential", "secret-", "private-output-body"} {
				assert.NotContains(t, string(serialized), secret)
			}
			if !tc.truncated {
				assert.Contains(t, row.StreamDetails.ErrorMessage, "Invalid key")
				assert.Contains(t, row.StreamDetails.ErrorMessage, "[redacted]")
			}
		})
	}
}

func TestRequestMonitorWebSocketErrorDetails(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{name: "abnormal close", err: &websocket.CloseError{Code: websocket.CloseAbnormalClosure, Text: "unexpected EOF"}, want: "websocket: close 1006 (abnormal closure): unexpected EOF"},
		{name: "close reason excludes peer text", err: &websocket.CloseError{Code: websocket.CloseInternalServerErr, Text: "secret-body Bearer secret-token"}, want: "websocket: close 1011 (internal server error)"},
		{name: "normal close before response completes", err: &websocket.CloseError{Code: websocket.CloseNormalClosure, Text: "secret-body"}, want: "websocket: close 1000 (normal)"},
		{name: "wrapped network reset excludes URL", err: &url.Error{Op: "Get", URL: "https://secret-user:secret-password@example.test/?key=secret-token", Err: &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}}, want: "connection reset by peer"},
		{name: "websocket message limit", err: fmt.Errorf("secret-wrapper: %w", websocket.ErrReadLimit), want: "websocket: read limit exceeded"},
		{name: "scanner buffer limit", err: bufio.ErrTooLong, want: "bufio.Scanner: token too long"},
		{name: "closed connection", err: net.ErrClosed, want: "use of closed network connection"},
		{name: "DNS lookup failure", err: &net.DNSError{Name: "secret-host", Err: "secret-message", IsNotFound: true}, want: "DNS host not found"},
		{name: "network timeout", err: &net.OpError{Op: "secret-operation", Err: syscall.ETIMEDOUT}, want: "network I/O timeout"},
		{name: "unknown error excludes body", err: errors.New("secret-body"), want: "transport error (details omitted)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(middleware.RequestId(), middleware.RouteTag("relay"))
			engine.GET("/ws", func(c *gin.Context) {
				service.MonitorWSConnected(c)
				service.MonitorWSMessage(c.Request.Context(), "upstream", true, 0, tc.err)
				service.MonitorWSShutdown(c.Request.Context(), websocket.CloseInternalServerErr)
				service.MonitorWSMessage(c.Request.Context(), "upstream", true, 0, net.ErrClosed)
				service.StartMonitorWSCall(c)
				status := relaycommon.NewStreamStatus()
				status.SetEndReason(relaycommon.StreamEndReasonScannerErr, tc.err)
				service.MarkRequestPolicySuccess(c, status)
			})
			engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ws", nil))
			rows := service.RequestMonitorSnapshots()
			require.GreaterOrEqual(t, len(rows), 2)
			assert.Equal(t, "ws_request", rows[0].Kind)
			assert.Equal(t, "failed", rows[0].State)
			serialized, err := common.Marshal(rows[:2])
			require.NoError(t, err)
			assert.NotContains(t, string(serialized), "secret")
			var diagnostic []map[string]any
			require.NoError(t, common.Unmarshal(serialized, &diagnostic))
			assert.Equal(t, tc.want, diagnostic[0]["stream_error"])
			ws := diagnostic[1]["websocket"].(map[string]any)
			assert.Equal(t, tc.want, ws["upstream"].(map[string]any)["error_detail"])
			assert.Equal(t, "upstream", ws["close_source"])
		})
	}
}

func TestRequestMonitorWebSocketHandshakeErrorDetails(t *testing.T) {
	engine := gin.New()
	engine.Use(middleware.RequestId(), middleware.RouteTag("relay"))
	engine.GET("/ws", func(c *gin.Context) {
		service.MonitorWSConnected(c)
		service.StartMonitorWSCall(c)
		service.MonitorWSHandshake(c, "wss://secret-user:secret-password@example.test/v1/responses?key=secret-token", time.Now(), &http.Response{
			StatusCode: http.StatusBadGateway,
		}, fmt.Errorf("secret-wrapper: %w", websocket.ErrBadHandshake))
		c.Status(http.StatusBadGateway)
	})
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ws", nil))
	rows := service.RequestMonitorSnapshots()
	require.GreaterOrEqual(t, len(rows), 2)
	for _, row := range rows[:2] {
		require.Len(t, row.Upstreams, 1)
		assert.Equal(t, http.StatusBadGateway, row.Upstreams[0].Status)
		assert.Equal(t, "websocket: bad handshake", row.Upstreams[0].ErrorDetail)
		serialized, err := common.Marshal(row)
		require.NoError(t, err)
		assert.NotContains(t, string(serialized), "secret")
	}
}

func TestRequestMonitorRunningRetentionFiltersAndAccess(t *testing.T) {
	engine := gin.New()
	engine.Use(middleware.RequestId())
	engine.GET("/relay", middleware.RouteTag("relay"), func(c *gin.Context) {
		assert.Equal(t, "running", service.RequestMonitorSnapshots()[0].State)
		c.Status(http.StatusNoContent)
	})
	var oldest string
	for i := range service.RequestMonitorCapacity + 1 {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/relay", nil))
		if i == 0 {
			oldest = response.Header().Get(common.RequestIdKey)
		}
	}
	rows := service.RequestMonitorSnapshots()
	require.Len(t, rows, service.RequestMonitorCapacity)
	for _, row := range rows {
		assert.NotEqual(t, oldest, row.ID)
	}
	engine.GET("/list", controller.GetRequestMonitor)
	engine.GET("/detail/:id", controller.GetRequestMonitorDetail)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/list?q="+rows[0].ID+"&state=completed", nil))
	var payload struct {
		Data struct {
			Items []service.MonitoredRequest `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	require.Len(t, payload.Data.Items, 1)
	assert.Equal(t, rows[0].ID, payload.Data.Items[0].ID)
	assert.Nil(t, payload.Data.Items[0].Upstreams)
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/detail/"+oldest, nil))
	assert.Equal(t, http.StatusNotFound, response.Code)
	// 与生产路由相同的 RootAuth 阻止匿名读取诊断数据。
	protected := gin.New()
	protected.GET("/requests", middleware.RootAuth(), controller.GetRequestMonitor)
	response = httptest.NewRecorder()
	protected.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/requests", nil))
	assert.NotEqual(t, http.StatusOK, response.Code)
	assert.NotContains(t, response.Body.String(), rows[0].ID)
}

func TestRequestMonitorObservesActiveUpstreamWithoutConsumingStream(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "data: last\n\n")
	}))
	defer upstream.Close()
	headers := make(chan struct{})
	finished := make(chan struct{})
	var received []byte
	var requestErr error
	engine := gin.New()
	engine.Use(middleware.RequestId(), middleware.RouteTag("relay"))
	engine.GET("/stream", func(c *gin.Context) {
		c.Set("id", 42)
		c.Set("username", "stream-user")
		c.Set("token_id", 81)
		c.Set("token_name", "stream-token")
		request, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, upstream.URL, nil)
		client := &http.Client{Transport: service.MonitorRelayTransport(c, http.DefaultTransport)}
		response, err := client.Do(request)
		requestErr = err
		close(headers)
		if err != nil {
			return
		}
		received, requestErr = io.ReadAll(response.Body)
		_ = response.Body.Close()
		c.Data(http.StatusOK, "text/event-stream", received)
	})
	go func() {
		defer close(finished)
		engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/stream", nil))
	}()
	<-headers
	active := service.RequestMonitorSnapshots()[0]
	// 先释放服务端，再做 fatal 断言，避免断言失败时测试服务器无法关闭。
	close(release)
	<-finished
	require.NoError(t, requestErr)
	assert.Equal(t, "running", active.State)
	assert.Equal(t, "stream-user", active.Username)
	assert.Equal(t, 42, active.UserID)
	assert.Equal(t, 81, active.TokenID)
	assert.Equal(t, "stream-token", active.TokenName)
	require.Len(t, active.Upstreams, 1)
	assert.Equal(t, 200, active.Upstreams[0].Status)
	assert.Empty(t, active.Upstreams[0].End)
	assert.Equal(t, "data: first\n\ndata: last\n\n", string(received))
	completed := service.RequestMonitorSnapshots()[0]
	assert.Equal(t, "completed", completed.State)
	assert.Equal(t, "eof", completed.Upstreams[0].End)
	assert.Empty(t, completed.Upstreams[0].ErrorDetail)
	assert.Equal(t, int64(len(received)), completed.Upstreams[0].Bytes)
}

func TestRequestMonitorIdentitySnapshotAndSearch(t *testing.T) {
	engine := gin.New()
	engine.Use(middleware.RequestId())
	engine.POST("/identity", middleware.RouteTag("relay"), func(c *gin.Context) {
		// 身份只来自上下文，客户端自行声明的名称不影响记录。
		if c.Query("anonymous") == "" {
			c.Set("id", 10001)
			c.Set("username", "监看用户-Alice")
			c.Set("token_id", 20002)
			c.Set("token_name", "监看-Codex-CLI")
			c.Set("token_key", "must-not-capture-token-key")
		}
		c.AbortWithStatus(http.StatusBadRequest)
	})
	engine.GET("/list", controller.GetRequestMonitor)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/identity", nil)
	request.Header.Set("X-Username", "spoofed-user")
	request.Header.Set("X-Token-Name", "spoofed-token")
	engine.ServeHTTP(response, request)
	id := response.Header().Get(common.RequestIdKey)
	detail, ok := service.RequestMonitorDetail(id)
	require.True(t, ok)
	assert.Equal(t, "failed", detail.State)
	assert.Equal(t, 10001, detail.UserID)
	assert.Equal(t, "监看用户-Alice", detail.Username)
	assert.Equal(t, 20002, detail.TokenID)
	assert.Equal(t, "监看-Codex-CLI", detail.TokenName)
	serialized, err := common.Marshal(detail)
	require.NoError(t, err)
	assert.NotContains(t, string(serialized), "must-not-capture-token-key")
	assert.NotContains(t, string(serialized), "spoofed")
	for _, query := range []string{"监看用户-alice", "监看-CODEX-cli"} {
		response = httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/list?q="+url.QueryEscape(query), nil))
		var payload struct {
			Data struct {
				Items []service.MonitoredRequest `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
		require.Len(t, payload.Data.Items, 1)
		assert.Equal(t, id, payload.Data.Items[0].ID)
		assert.Equal(t, detail.Username, payload.Data.Items[0].Username)
		assert.Equal(t, detail.TokenName, payload.Data.Items[0].TokenName)
	}
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/identity?anonymous=1", nil))
	anonymous, ok := service.RequestMonitorDetail(response.Header().Get(common.RequestIdKey))
	require.True(t, ok)
	assert.Zero(t, anonymous.UserID)
	assert.Empty(t, anonymous.Username)
	assert.Zero(t, anonymous.TokenID)
	assert.Empty(t, anonymous.TokenName)
}

func TestRequestMonitorCapturesBodiesWithoutChangingRelay(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{name: "普通正文", text: "你好"},
		{name: "超过采集上限仍完整转发", text: strings.Repeat("x", service.RequestMonitorBodyLimit+256)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := `{"model":"client-model","input":"` + tc.text + `"}`
			upstreamInput := `{"model":"mapped-model","input":"` + tc.text + `"}`
			output := "data: {\"delta\":\"" + tc.text + "\"}\n\ndata: [DONE]\n\n"
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				assert.Equal(t, upstreamInput, string(data))
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, output)
			}))
			defer upstream.Close()
			engine := gin.New()
			engine.Use(middleware.RequestId())
			engine.POST("/relay", middleware.RouteTag("relay"), func(c *gin.Context) {
				data, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				assert.Equal(t, input, string(data))
				c.Set("channel_id", 5)
				req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstream.URL, strings.NewReader(upstreamInput))
				require.NoError(t, err)
				req.Header.Set("Authorization", "Bearer must-not-capture-header")
				req.Header.Set("Content-Type", "application/json")
				client := &http.Client{Transport: service.MonitorRelayTransport(c, http.DefaultTransport)}
				response, err := client.Do(req)
				require.NoError(t, err)
				defer response.Body.Close()
				c.Header("Content-Type", response.Header.Get("Content-Type"))
				_, err = io.Copy(c.Writer, response.Body)
				require.NoError(t, err)
			})
			engine.GET("/list", controller.GetRequestMonitor)
			engine.GET("/detail/:id", controller.GetRequestMonitorDetail)
			request := httptest.NewRequest(http.MethodPost, "/relay", strings.NewReader(input))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			assert.Equal(t, output, response.Body.String())
			id := response.Header().Get(common.RequestIdKey)
			detail, ok := service.RequestMonitorDetail(id)
			require.True(t, ok)
			require.Len(t, detail.Bodies, 4)
			byDirection := make(map[string]service.MonitoredBody)
			for _, body := range detail.Bodies {
				byDirection[body.Direction] = body
			}
			for direction, expected := range map[string]string{"request": input, "upstream_request": upstreamInput, "upstream_response": output, "response": output} {
				body := byDirection[direction]
				assert.Equal(t, expected[:min(len(expected), service.RequestMonitorBodyLimit)], body.Content)
				assert.Equal(t, int64(len(expected)), body.TotalBytes)
				assert.Equal(t, len(expected) > service.RequestMonitorBodyLimit, body.Truncated)
			}
			assert.Equal(t, 5, byDirection["upstream_response"].ChannelID)
			assert.Equal(t, service.RequestMonitorRetention, detail.ExpiresAt.Sub(detail.StartedAt))
			response = httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/list?q="+id, nil))
			assert.NotContains(t, response.Body.String(), `"bodies"`)
			response = httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/detail/"+id, nil))
			assert.Equal(t, http.StatusOK, response.Code)
			assert.Contains(t, response.Body.String(), `"bodies"`)
			assert.NotContains(t, response.Body.String(), "must-not-capture-header")
		})
	}
}
