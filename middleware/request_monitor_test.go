package middleware_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
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
				status.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
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
	engine.GET("/cancel", func(c *gin.Context) { c.Status(http.StatusOK) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/cancel", nil).WithContext(ctx))
	row = service.RequestMonitorSnapshots()[0]
	assert.Equal(t, "failed", row.State)
	assert.Equal(t, "canceled", row.ClientError)
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
	require.Len(t, active.Upstreams, 1)
	assert.Equal(t, 200, active.Upstreams[0].Status)
	assert.Empty(t, active.Upstreams[0].End)
	assert.Equal(t, "data: first\n\ndata: last\n\n", string(received))
	completed := service.RequestMonitorSnapshots()[0]
	assert.Equal(t, "completed", completed.State)
	assert.Equal(t, "eof", completed.Upstreams[0].End)
	assert.Equal(t, int64(len(received)), completed.Upstreams[0].Bytes)
}
