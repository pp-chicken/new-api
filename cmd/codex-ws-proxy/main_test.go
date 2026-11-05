package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/networktest"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkStabilityUsesOneConnectionAndSmallProbes(t *testing.T) {
	var connections, checks atomic.Int32
	finished := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		assert.Equal(t, "/v1/network-test", r.URL.Path)
		assert.Equal(t, "5300", r.URL.Query().Get("duration_ms"))
		assert.Empty(t, r.URL.Query().Get("bytes"))
		upgrader := websocket.Upgrader{Subprotocols: []string{networktest.StabilitySubprotocol}}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			finished <- err
			return
		}
		defer conn.Close()
		finished <- networktest.ServeStability(conn, 10*time.Second, func() error {
			checks.Add(1)
			return nil
		})
	}))
	defer server.Close()
	var output bytes.Buffer
	require.NoError(t, runNetworkCheck(context.Background(), server.URL, "secret", networktest.MaxBytes, time.Second, 5300*time.Millisecond, &output))
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("客户端测试结束后服务端没有释放连接")
	}
	assert.EqualValues(t, 1, connections.Load())
	assert.EqualValues(t, 2, checks.Load())
	assert.Contains(t, output.String(), "探测次数：2；成功：2；失败：0；结束时中止：0")
	assert.Contains(t, output.String(), "上传 2048 字节，回传 2048 字节")
	assert.Contains(t, output.String(), "达到设定时长")
	assert.NotContains(t, output.String(), "MiB/s")
	assert.NotContains(t, output.String(), "secret")
}

func TestNetworkStabilityStopsAndSummarizes(t *testing.T) {
	for _, tc := range []struct {
		mode, expected string
	}{
		{"断开", "失败：1；结束时中止：0"},
		{"间隔中断开", "失败：0；结束时中止：0"},
		{"错误回包", "失败：1；结束时中止：0"},
		{"探测超时", "失败：1；结束时中止：0"},
		{"总时长截止", "失败：0；结束时中止：1"},
		{"用户中止", "失败：0；结束时中止：1"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var connections atomic.Int32
			done := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				connections.Add(1)
				upgrader := websocket.Upgrader{Subprotocols: []string{networktest.StabilitySubprotocol}}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				_ = conn.WriteMessage(websocket.TextMessage, []byte("stability-ready 1024 5000"))
				_, payload, err := conn.ReadMessage()
				if err != nil {
					return
				}
				switch tc.mode {
				case "断开":
					return
				case "间隔中断开":
					_ = conn.WriteMessage(websocket.BinaryMessage, payload)
					return
				case "错误回包":
					payload[0] ^= 1
					_ = conn.WriteMessage(websocket.BinaryMessage, payload)
				case "用户中止":
					cancel()
				}
				_, _, _ = conn.ReadMessage()
			}))
			defer server.Close()
			timeout := 2 * time.Second
			if tc.mode == "探测超时" {
				timeout = 100 * time.Millisecond
			}
			var output bytes.Buffer
			err := runNetworkCheck(ctx, server.URL, "secret", networktest.DefaultBytes, timeout, time.Second, &output)
			require.Error(t, err)
			<-done
			assert.EqualValues(t, 1, connections.Load())
			assert.Contains(t, output.String(), "长连接稳定性汇总")
			assert.Contains(t, output.String(), tc.expected)
			assert.Contains(t, output.String(), "重连：0 次")
			if tc.mode == "间隔中断开" {
				assert.Contains(t, output.String(), "失败阶段：间隔等待")
				assert.Contains(t, output.String(), "连接在探测间隔中发生异常")
			} else {
				assert.NotContains(t, output.String(), "已完成探测成功率：100.00%")
			}
			if tc.mode == "总时长截止" {
				assert.Contains(t, output.String(), "达到设定时长")
				assert.NotContains(t, output.String(), "失败阶段：")
			}
		})
	}
}

func TestNetworkStabilitySummaryStatistics(t *testing.T) {
	started := time.Date(2026, 11, 5, 1, 0, 0, 0, time.UTC)
	summary := stabilitySummary{
		started: started, lastSuccess: started.Add(20 * time.Second),
		attempts: 6, failed: 1, interrupted: 1,
		rtts: []time.Duration{10 * time.Millisecond, 30 * time.Millisecond, 20 * time.Millisecond, 100 * time.Millisecond},
	}
	var output bytes.Buffer
	summary.write(&output, time.Minute, started.Add(25*time.Second), "测试结束")
	assert.Contains(t, output.String(), "探测次数：6；成功：4；失败：1；结束时中止：1")
	assert.Contains(t, output.String(), "已完成探测成功率：80.00%")
	assert.Contains(t, output.String(), "最小 10ms；平均 40ms；P95 100ms；最大 100ms")
	assert.Contains(t, output.String(), "相邻探测 RTT 最大变化：80ms")
	assert.Contains(t, output.String(), "距测试结束：5s")
	assert.Contains(t, output.String(), "上传 4096 字节，回传 4096 字节")
}

func TestNetworkStabilityRejectsInvalidDurationAndUnsupportedServer(t *testing.T) {
	for _, duration := range []time.Duration{-time.Second, time.Millisecond, 25 * time.Hour} {
		var output bytes.Buffer
		err := runNetworkCheck(context.Background(), "wss://example.com/v1", "secret", networktest.DefaultBytes, time.Second, duration, &output)
		require.ErrorContains(t, err, "时长")
		assert.Empty(t, output.String())
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	var output bytes.Buffer
	err := runNetworkCheck(context.Background(), server.URL, "secret", networktest.DefaultBytes, time.Second, time.Minute, &output)
	require.ErrorContains(t, err, "请部署支持长连接稳定性协议")
}

func TestNetworkStabilityServerRejectsBurstAndReplay(t *testing.T) {
	for _, sequence := range []uint64{1, 2} {
		t.Run(fmt.Sprint(sequence), func(t *testing.T) {
			finished := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upgrader := websocket.Upgrader{}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					finished <- err
					return
				}
				defer conn.Close()
				finished <- networktest.ServeStability(conn, time.Second, func() error { return nil })
			}))
			defer server.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer conn.Close()
			require.NoError(t, networktest.AwaitStabilityReady(conn, time.Second))
			_, err = networktest.ProbeStability(conn, 1, time.Second)
			require.NoError(t, err)
			_, err = networktest.ProbeStability(conn, sequence, time.Second)
			require.Error(t, err)
			require.Error(t, <-finished)
		})
	}
}

func TestNetworkTestActualUploadAndDownload(t *testing.T) {
	const size = 249027
	finished := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/network-test", r.URL.Path)
		assert.Equal(t, "249027", r.URL.Query().Get("bytes"))
		assert.Equal(t, "Bearer testsecret", r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("Sec-WebSocket-Extensions"))
		upgrader := websocket.Upgrader{Subprotocols: []string{networktest.Subprotocol}}
		conn, err := upgrader.Upgrade(w, r, http.Header{"X-Oneapi-Request-Id": {"network-request-id"}})
		if err != nil {
			finished <- err
			return
		}
		defer conn.Close()
		finished <- networktest.Serve(conn, size, time.Second)
	}))
	defer server.Close()
	var output bytes.Buffer
	require.NoError(t, runNetworkTest(context.Background(), server.URL+"/v1/responses", "testsecret", size, time.Second, &output))
	require.NoError(t, <-finished)
	assert.Contains(t, output.String(), "上传（服务端已确认并校验）：249027 字节")
	assert.Contains(t, output.String(), "下载（已收齐并校验）：249027 字节")
	assert.Contains(t, output.String(), "network-request-id")
	assert.Contains(t, output.String(), "SHA-256 校验通过")
	assert.NotContains(t, output.String(), "testsecret")
}

func TestNetworkTestRejectsInvalidOptions(t *testing.T) {
	for _, tc := range []struct {
		name, target, key string
		size              int
		timeout           time.Duration
	}{
		{"缺少令牌", "wss://example.com/v1", "", networktest.MinBytes, time.Second},
		{"远程明文", "http://example.com/v1", "secret", networktest.MinBytes, time.Second},
		{"URL 包含令牌", "wss://example.com/v1?key=secret", "secret", networktest.MinBytes, time.Second},
		{"URL 包含密码", "wss://user:secret@example.com/v1", "secret", networktest.MinBytes, time.Second},
		{"字节数太小", "wss://example.com/v1", "secret", networktest.MinBytes - 1, time.Second},
		{"字节数太大", "wss://example.com/v1", "secret", networktest.MaxBytes + 1, time.Second},
		{"无超时", "wss://example.com/v1", "secret", networktest.MinBytes, 0},
		{"超时太长", "wss://example.com/v1", "secret", networktest.MinBytes, networktest.MaxPhaseTimeout + time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			err := runNetworkTest(context.Background(), tc.target, tc.key, tc.size, tc.timeout, &output)
			require.Error(t, err)
			assert.Empty(t, output.String())
			assert.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestNetworkTestHandshakeErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		hint   string
	}{{404, "请先部署"}, {401, "令牌"}, {429, "限流"}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			var output bytes.Buffer
			err := runNetworkTest(context.Background(), server.URL, "secret", networktest.MinBytes, time.Second, &output)
			require.ErrorContains(t, err, tc.hint)
			assert.NotContains(t, output.String(), "测速完成")
		})
	}
}

func TestNetworkTestRejectsUntrustedTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("证书未通过验证时不应发送认证请求")
	}))
	defer server.Close()
	var output bytes.Buffer
	err := runNetworkTest(context.Background(), server.URL, "secret", networktest.MinBytes, time.Second, &output)
	require.ErrorContains(t, err, "TLS阶段失败")
	assert.NotContains(t, output.String(), "secret")
}

func TestNetworkTestDoesNotReportIncompleteTransfersAsSuccess(t *testing.T) {
	for _, failure := range []string{"协议未协商", "上传摘要错误", "上传确认超时", "下载截断", "下载摘要错误", "等待服务端超时"} {
		t.Run(failure, func(t *testing.T) {
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upgrader := websocket.Upgrader{Subprotocols: []string{networktest.Subprotocol}}
				if failure == "协议未协商" {
					upgrader.Subprotocols = nil
				}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				if failure == "协议未协商" || failure == "等待服务端超时" {
					<-release
					return
				}
				_ = conn.SetReadDeadline(time.Now().Add(time.Second))
				_ = conn.WriteMessage(websocket.TextMessage, fmt.Appendf(nil, "ready %d", networktest.MinBytes))
				_, _, _ = conn.ReadMessage()
				_ = conn.WriteMessage(websocket.TextMessage, []byte("pong"))
				_, payload, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if failure == "上传确认超时" {
					<-release
					return
				}
				if failure == "上传摘要错误" {
					_ = conn.WriteMessage(websocket.TextMessage, []byte("uploaded invalid"))
					return
				}
				digest := sha256.Sum256(payload)
				_ = conn.WriteMessage(websocket.TextMessage, fmt.Appendf(nil, "uploaded %x", digest))
				_, _, _ = conn.ReadMessage()
				if failure == "下载截断" {
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte("short"))
					return
				}
				_ = conn.WriteMessage(websocket.BinaryMessage, payload)
				_ = conn.WriteMessage(websocket.TextMessage, []byte("downloaded invalid"))
			}))
			defer server.Close()
			defer close(release)
			var output bytes.Buffer
			timeout := time.Second
			if strings.Contains(failure, "超时") {
				timeout = 50 * time.Millisecond
			}
			err := runNetworkTest(context.Background(), server.URL, "secret", networktest.MinBytes, timeout, &output)
			require.Error(t, err)
			assert.NotContains(t, output.String(), "测速完成")
			assert.NotContains(t, output.String(), "下载（已收齐并校验）")
			if failure == "上传摘要错误" || failure == "上传确认超时" {
				assert.NotContains(t, output.String(), "上传（服务端已确认并校验）")
			}
		})
	}
}

func TestNetworkTestServerBoundsUpload(t *testing.T) {
	for _, failure := range []string{"上传超限", "上传截断", "控制消息超限", "上传超时"} {
		t.Run(failure, func(t *testing.T) {
			finished := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upgrader := websocket.Upgrader{}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					finished <- err
					return
				}
				defer conn.Close()
				timeout := time.Second
				if failure == "上传超时" {
					timeout = 50 * time.Millisecond
				}
				finished <- networktest.Serve(conn, networktest.MinBytes, timeout)
			}))
			defer server.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer conn.Close()
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
			_, _, err = conn.ReadMessage()
			require.NoError(t, err)
			if failure == "控制消息超限" {
				require.NoError(t, conn.WriteMessage(websocket.TextMessage, make([]byte, 129)))
			} else {
				require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("ping")))
				_, _, err = conn.ReadMessage()
				require.NoError(t, err)
				switch failure {
				case "上传超限":
					_ = conn.WriteMessage(websocket.BinaryMessage, make([]byte, networktest.MinBytes+1))
				case "上传截断":
					require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("short")))
				}
			}
			select {
			case err := <-finished:
				require.Error(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("服务端未及时结束失败的测速")
			}
		})
	}
}

type tunnelFixture struct {
	client   *websocket.Conn
	upstream *websocket.Conn
	logs     *bytes.Buffer
	finished <-chan struct{}
}

func newTunnelFixture(t *testing.T, verbose bool) *tunnelFixture {
	t.Helper()
	type accepted struct {
		conn *websocket.Conn
		err  error
	}
	incoming := make(chan accepted, 1)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, http.Header{"X-Oneapi-Request-Id": {"new-api-parent-test"}, "X-Request-Id": {"other-request-id"}})
		incoming <- accepted{conn, err}
		<-release
	}))
	p, err := newProxy(upstream.URL+"/v1", verbose)
	require.NoError(t, err)
	logs := &bytes.Buffer{}
	p.logger = log.New(logs, "", 0)
	finished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		p.responses(w, r)
	}))
	fixture := &tunnelFixture{logs: logs, finished: finished}
	t.Cleanup(func() {
		if fixture.client != nil {
			_ = fixture.client.Close()
		}
		if fixture.upstream != nil {
			_ = fixture.upstream.Close()
		}
		close(release)
		server.Close()
		upstream.Close()
	})
	fixture.client, _, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", http.Header{"Authorization": {"Bearer secret-test-token"}})
	require.NoError(t, err)
	select {
	case result := <-incoming:
		require.NoError(t, result.err)
		fixture.upstream = result.conn
	case <-time.After(5 * time.Second):
		t.Fatal("代理没有建立上游连接")
	}
	require.NoError(t, fixture.client.SetReadDeadline(time.Now().Add(5*time.Second)))
	require.NoError(t, fixture.upstream.SetReadDeadline(time.Now().Add(5*time.Second)))
	return fixture
}

func (f *tunnelFixture) wait(t *testing.T) string {
	t.Helper()
	select {
	case <-f.finished:
		return f.logs.String()
	case <-time.After(5 * time.Second):
		t.Fatal("代理没有完成连接清理")
		return ""
	}
}

func TestTunnelRelaysCloseFrames(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		code   int
		reason string
	}{
		{"客户端正常关闭", "client", 1000, "正常结束"},
		{"上游结束空闲连接", "upstream", 1001, "idle connection"},
		{"上游策略关闭", "upstream", 1008, "policy changed"},
		{"自定义关闭码", "client", 4001, "session expired"},
		{"无状态的关闭帧", "client", 1005, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTunnelFixture(t, true)
			sender, receiver := f.client, f.upstream
			if tc.source == "upstream" {
				sender, receiver = f.upstream, f.client
			}
			require.NoError(t, sender.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(tc.code, tc.reason), time.Now().Add(time.Second)))
			_, _, err := receiver.ReadMessage()
			var closeErr *websocket.CloseError
			require.ErrorAs(t, err, &closeErr)
			assert.Equal(t, tc.code, closeErr.Code)
			assert.Equal(t, tc.reason, closeErr.Text)
			logs := f.wait(t)
			assert.Contains(t, logs, "event=connection_closing source="+tc.source+" operation=read")
			assert.Contains(t, logs, fmt.Sprintf("original_close_code=%d", tc.code))
			assert.Contains(t, logs, fmt.Sprintf("close_reason=%q", tc.reason))
			assert.Equal(t, 1, strings.Count(logs, "event=connection_closing"))
		})
	}
}

func TestTunnelReportsAbnormalDisconnect(t *testing.T) {
	for _, source := range []string{"client", "upstream"} {
		t.Run(source, func(t *testing.T) {
			f := newTunnelFixture(t, true)
			sender, receiver := f.client, f.upstream
			if source == "upstream" {
				sender, receiver = f.upstream, f.client
			}
			require.NoError(t, sender.Close())
			_, _, err := receiver.ReadMessage()
			var closeErr *websocket.CloseError
			require.ErrorAs(t, err, &closeErr)
			assert.Equal(t, websocket.CloseInternalServerErr, closeErr.Code)
			assert.Equal(t, "proxy transport error", closeErr.Text)
			logs := f.wait(t)
			assert.Contains(t, logs, "event=connection_closing source="+source+" operation=read original_close_code=1006")
			assert.Contains(t, logs, "unexpected EOF")
			assert.Equal(t, 1, strings.Count(logs, "event=connection_closing"))
		})
	}
}

func TestTunnelKeepsConnectionAcrossResponsesAndLogsTraffic(t *testing.T) {
	f := newTunnelFixture(t, true)
	requests := []string{
		`{"type":"response.create","input":"secret-prompt-one"}`,
		`{"type":"response.create","input":"secret-prompt-two"}`,
	}
	response := `{"type":"response.completed","response":{"output":"secret-model-output"}}`
	for _, request := range requests {
		require.NoError(t, f.client.WriteMessage(websocket.TextMessage, []byte(request)))
		kind, received, err := f.upstream.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, websocket.TextMessage, kind)
		assert.Equal(t, request, string(received))
		require.NoError(t, f.upstream.WriteMessage(websocket.TextMessage, []byte(response)))
		_, received, err = f.client.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, response, string(received))
	}
	require.NoError(t, f.client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1000, "done"), time.Now().Add(time.Second)))
	_, _, err := f.upstream.ReadMessage()
	require.True(t, websocket.IsCloseError(err, 1000), "%v", err)
	logs := f.wait(t)
	assert.Contains(t, logs, `upstream_request_id="new-api-parent-test"`)
	assert.Equal(t, 1, strings.Count(logs, "event=dial_started"))
	assert.Equal(t, 1, strings.Count(logs, "event=first_message side=client"))
	assert.Equal(t, 1, strings.Count(logs, "event=first_message side=upstream"))
	assert.Contains(t, logs, "side=client message=2")
	assert.Contains(t, logs, "gap_ms=")
	assert.Contains(t, logs, fmt.Sprintf("client_messages_in=2 client_bytes_in=%d client_messages_out=2 client_bytes_out=%d", len(requests[0])+len(requests[1]), 2*len(response)))
	assert.Contains(t, logs, fmt.Sprintf("upstream_messages_in=2 upstream_bytes_in=%d upstream_messages_out=2 upstream_bytes_out=%d", 2*len(response), len(requests[0])+len(requests[1])))
	firstID := strings.Fields(logs)[0]
	for line := range strings.SplitSeq(strings.TrimSpace(logs), "\n") {
		assert.True(t, strings.HasPrefix(line, firstID+" "), line)
	}
	for _, secret := range []string{"secret-prompt-one", "secret-prompt-two", "secret-model-output", "secret-test-token", "Authorization"} {
		assert.NotContains(t, logs, secret)
	}
}

func TestTunnelVerboseDisabled(t *testing.T) {
	f := newTunnelFixture(t, false)
	require.NoError(t, f.client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1000, "done"), time.Now().Add(time.Second)))
	_, _, err := f.upstream.ReadMessage()
	require.True(t, websocket.IsCloseError(err, 1000), "%v", err)
	assert.Empty(t, f.wait(t))
}

func TestTunnelLogsHandshakeFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Oneapi-Request-Id", "rejected-parent")
		http.Error(w, "private-error-body", http.StatusForbidden)
	}))
	defer upstream.Close()
	p, err := newProxy(upstream.URL+"/v1", true)
	require.NoError(t, err)
	logs := &bytes.Buffer{}
	p.logger = log.New(logs, "", 0)
	finished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		p.responses(w, r)
	}))
	defer server.Close()
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
	require.NoError(t, err)
	defer client.Close()
	require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, _, err = client.ReadMessage()
	assert.True(t, websocket.IsCloseError(err, websocket.CloseServiceRestart), "%v", err)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("握手失败后代理没有退出")
	}
	assert.Contains(t, logs.String(), `status=403 upstream_request_id="rejected-parent"`)
	assert.Contains(t, logs.String(), "event=connection_closing source=upstream operation=handshake")
	assert.NotContains(t, logs.String(), "private-error-body")
}

func TestHTTPConversionClosesEachUpstreamNormally(t *testing.T) {
	for _, stream := range []bool{true, false} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			type observation struct {
				request string
				err     error
			}
			observed := make(chan observation, 2)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upgrader := websocket.Upgrader{}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					observed <- observation{err: err}
					return
				}
				defer conn.Close()
				_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
				_, request, err := conn.ReadMessage()
				if err == nil {
					err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"test-response","status":"completed"}}`))
				}
				if err == nil {
					_, _, err = conn.ReadMessage()
				}
				observed <- observation{string(request), err}
			}))
			defer upstream.Close()
			p, err := newProxy(upstream.URL+"/v1", true)
			require.NoError(t, err)
			logs := &bytes.Buffer{}
			p.logger = log.New(logs, "", 0)
			for i := range 2 {
				request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"test","input":"request-%d","stream":%t}`, i, stream)))
				response := httptest.NewRecorder()
				p.responses(response, request)
				assert.Equal(t, http.StatusOK, response.Code)
				assert.Contains(t, response.Body.String(), "test-response")
				if stream {
					assert.Contains(t, response.Body.String(), "event: response.completed")
				}
				select {
				case result := <-observed:
					assert.Contains(t, result.request, `"type":"response.create"`)
					assert.True(t, websocket.IsCloseError(result.err, 1000), "%v", result.err)
				case <-time.After(5 * time.Second):
					t.Fatal("HTTP 请求完成后没有关闭上游连接")
				}
			}
			assert.Equal(t, 2, strings.Count(logs.String(), "event=dial_started"))
			assert.Equal(t, 2, strings.Count(logs.String(), "event=connection_finished"))
		})
	}
}

func TestCloseStatusDoesNotSendReservedCodes(t *testing.T) {
	for _, code := range []int{1004, 1006, 1015, 2000, 5000} {
		actual, reason := wsCloseStatus(&websocket.CloseError{Code: code, Text: "internal detail"})
		assert.Equal(t, 1011, actual)
		assert.Equal(t, "proxy transport error", reason)
	}
	for _, err := range []error{io.EOF, errors.New("write failure")} {
		actual, _ := wsCloseStatus(err)
		assert.Equal(t, 1011, actual)
	}
}

type failingStreamWriter struct {
	*httptest.ResponseRecorder
	err error
}

func (w *failingStreamWriter) FlushError() error { return w.err }

func TestHTTPStreamLogsFlushFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, _, err = conn.ReadMessage()
		if err == nil {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created"}`))
			_, _, _ = conn.ReadMessage()
		}
	}))
	defer upstream.Close()
	p, err := newProxy(upstream.URL+"/v1", true)
	require.NoError(t, err)
	logs := &bytes.Buffer{}
	p.logger = log.New(logs, "", 0)
	writer := &failingStreamWriter{httptest.NewRecorder(), errors.New("client flush failed")}
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test","stream":true}`))
	p.responses(writer, request)
	assert.Contains(t, logs.String(), "event=connection_closing source=client operation=flush")
	assert.Contains(t, logs.String(), `error="client flush failed"`)
	assert.Contains(t, logs.String(), "event=close_sent side=upstream close_code=1011")
	assert.Contains(t, logs.String(), "client_messages_out=0")
}
