package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/pkg/networktest"
	"github.com/gorilla/websocket"
)

func runNetworkTest(ctx context.Context, target, key string, size int, timeout time.Duration, output io.Writer) error {
	if err := networktest.Validate(size, timeout); err != nil {
		return err
	}
	u, err := url.Parse(strings.TrimSpace(target))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return fmt.Errorf("测速目标必须是无用户信息、查询参数或片段的 HTTP(S)/WS(S) 地址")
	}
	switch u.Scheme {
	case "https", "wss":
	case "http", "ws":
		if u.Hostname() != "localhost" && !net.ParseIP(u.Hostname()).IsLoopback() {
			return fmt.Errorf("远程测速必须使用 https:// 或 wss://；明文连接只允许本机回环地址")
		}
	default:
		return fmt.Errorf("测速目标协议必须是 http、https、ws 或 wss")
	}
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, "\r\n") {
		return fmt.Errorf("请通过 CODEX_WS_PROXY_API_KEY 或 OPENAI_API_KEY 环境变量提供 New API 令牌")
	}
	p, err := newProxy(target, false)
	if err != nil {
		return err
	}
	u = p.baseURL
	u.Scheme = wsScheme(u.Scheme)
	u.Path = strings.TrimRight(u.Path, "/") + networktest.Path
	u.RawPath = ""
	u.RawQuery = "bytes=" + strconv.Itoa(size)
	fmt.Fprintf(output, "测速目标：%s\n每个方向：%d 字节（%.3f MiB）；每阶段超时：%s\n", u, size, float64(size)/(1<<20), timeout)
	fmt.Fprintln(output, "链路：本机代理进程 → New API；连接方式与代理上游一致，不读取 HTTP_PROXY/HTTPS_PROXY。")

	dialer := p.dialer
	dialer.HandshakeTimeout = timeout
	dialer.Subprotocols = []string{networktest.Subprotocol}
	dialer.EnableCompression = false
	phase := "DNS/TCP"
	var transportTime, tlsTime time.Duration
	var tlsStarted, upgradeStarted time.Time
	dialer.NetDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		started := time.Now()
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		transportTime = time.Since(started)
		if err == nil {
			phase = "WebSocket 握手"
			upgradeStarted = time.Now()
		}
		return conn, err
	}
	trace := &httptrace.ClientTrace{
		TLSHandshakeStart: func() { phase = "TLS"; tlsStarted = time.Now() },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			tlsTime = time.Since(tlsStarted)
			if err == nil {
				phase = "WebSocket 握手"
				upgradeStarted = time.Now()
			}
		},
	}
	started := time.Now()
	conn, response, err := dialer.DialContext(httptrace.WithClientTrace(ctx, trace), u.String(), http.Header{"Authorization": {"Bearer " + key}})
	connected := time.Now()
	fmt.Fprintf(output, "建连总耗时：%s；DNS+TCP：%s；TLS：%s\n", connected.Sub(started).Round(time.Microsecond), transportTime.Round(time.Microsecond), tlsTime.Round(time.Microsecond))
	if !upgradeStarted.IsZero() && phase == "WebSocket 握手" {
		fmt.Fprintf(output, "WebSocket Upgrade：%s\n", connected.Sub(upgradeStarted).Round(time.Microsecond))
	}
	if response != nil {
		if response.Body != nil {
			defer response.Body.Close()
		}
		id := response.Header.Get("X-Oneapi-Request-Id")
		if id == "" {
			id = response.Header.Get("X-Request-Id")
		}
		fmt.Fprintf(output, "握手状态：%d；New API 请求 ID：%q\n", response.StatusCode, id)
	}
	if err != nil {
		if response != nil {
			switch response.StatusCode {
			case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusOK:
				return fmt.Errorf("测速握手失败（HTTP %d）：请先部署包含 /v1/network-test 的 New API，并确认反向代理允许该 WebSocket 路由", response.StatusCode)
			case http.StatusUnauthorized, http.StatusForbidden:
				return fmt.Errorf("测速认证或访问检查失败（HTTP %d），请检查 New API 令牌和 IP 限制", response.StatusCode)
			case http.StatusTooManyRequests:
				return fmt.Errorf("测速被限流或已有测试占用连接，请稍后重试（Retry-After=%q）", response.Header.Get("Retry-After"))
			}
		}
		return fmt.Errorf("%s阶段失败: %w", phase, err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if conn.Subprotocol() != networktest.Subprotocol {
		return fmt.Errorf("服务端没有协商测速协议，请检查 New API 版本及反向代理路由")
	}
	fmt.Fprintln(output, "开始往返、上传和下载测试……")
	result, err := networktest.Run(conn, size, timeout)
	if result.RTT > 0 {
		fmt.Fprintf(output, "应用层往返 RTT：%s（单次）\n", result.RTT.Round(time.Microsecond))
	}
	if result.UploadWrite > 0 {
		fmt.Fprintf(output, "上传本机写入耗时：%s（含随机数据生成；不代表服务端已收齐）\n", result.UploadWrite.Round(time.Microsecond))
	}
	for _, direction := range []struct {
		name string
		took time.Duration
	}{{"上传（服务端已确认并校验）", result.Upload}, {"下载（已收齐并校验）", result.Download}} {
		if direction.took > 0 {
			bytesPerSecond := float64(size) / direction.took.Seconds()
			fmt.Fprintf(output, "%s：%d 字节 / %s = %.3f MiB/s（%.3f Mbps）\n", direction.name, size, direction.took.Round(time.Microsecond), bytesPerSecond/(1<<20), bytesPerSecond*8/1e6)
		}
	}
	if err != nil {
		return err
	}
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "测速完成"), time.Now().Add(time.Second))
	fmt.Fprintln(output, "测速完成：两个方向的字节数和 SHA-256 校验通过。结果为应用层有效吞吐，包含协议往返及数据生成/校验开销。")
	return nil
}
