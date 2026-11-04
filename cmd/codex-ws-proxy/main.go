// Command codex-ws-proxy exposes an OpenAI-compatible local endpoint.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/pkg/networktest"
	"github.com/gorilla/websocket"
)

const (
	defaultListen = "127.0.0.1:28080"
	defaultTarget = "wss://ai.jizw.net/v1"
	launchdLabel  = "com.codex.ws-proxy"
)

type proxy struct {
	baseURL  *url.URL
	wsURL    string
	verbose  bool
	logger   *log.Logger
	sequence atomic.Uint64
	dialer   websocket.Dialer
	upgrader websocket.Upgrader
	client   *http.Client
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	listen := flag.String("listen", envOr("CODEX_WS_PROXY_LISTEN", defaultListen), "本地监听地址")
	target := flag.String("target", envOr("NEW_API_RESPONSES_WS", defaultTarget), "上游 OpenAI 兼容地址，/v1/responses 可省略")
	verbose := flag.Bool("verbose", false, "输出详细请求、响应和 WebSocket 日志")
	install := flag.Bool("install-service", false, "安装并启动 macOS 用户级 launchd 服务")
	uninstall := flag.Bool("uninstall-service", false, "停止并卸载 macOS 用户级 launchd 服务")
	status := flag.Bool("service-status", false, "查看 macOS 用户级 launchd 服务状态")
	networkTest := flag.Bool("network-test", false, "测试到 New API 的 WebSocket 上传/下载链路后退出（需要服务端支持）")
	testBytes := flag.Int("test-bytes", networktest.DefaultBytes, "测速时每个方向的字节数，范围 65536～33554432")
	testTimeout := flag.Duration("test-timeout", networktest.DefaultTimeout, "测速每阶段超时，最大 2m")
	flag.Parse()
	if *networkTest {
		if *install || *uninstall || *status {
			log.Fatal("-network-test 不能与服务管理参数同时使用")
		}
		key := envOr("CODEX_WS_PROXY_API_KEY", envOr("OPENAI_API_KEY", ""))
		if err := runNetworkTest(context.Background(), *target, key, *testBytes, *testTimeout, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *install || *uninstall || *status {
		if *install && (*uninstall || *status) || *uninstall && *status {
			log.Fatal("-install-service、-uninstall-service、-service-status 只能选择一个")
		}
		if *install {
			if err := installService(*listen, *target, *verbose); err != nil {
				log.Fatal(err)
			}
			return
		}
		if *uninstall {
			if err := uninstallService(); err != nil {
				log.Fatal(err)
			}
			return
		}
		if err := serviceStatus(); err != nil {
			log.Fatal(err)
		}
		return
	}
	p, err := newProxy(*target, *verbose)
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", p.health)
	mux.HandleFunc("/v1/responses", p.responses)
	mux.HandleFunc("/v1/", p.forwardHTTP)
	mux.HandleFunc("/", p.forwardHTTP)
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 15 * time.Second}
	p.logf("listening on http://%s; upstream base=%s; responses websocket=%s", *listen, p.baseURL, p.wsURL)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func newProxy(target string, verbose bool) (*proxy, error) {
	u, err := url.Parse(strings.TrimSpace(target))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid -target %q", target)
	}
	base := *u
	if strings.HasSuffix(base.Path, "/responses") {
		base.Path = strings.TrimSuffix(base.Path, "/responses")
	}
	if base.Path == "" {
		base.Path = "/v1"
	}
	base.Scheme = httpScheme(base.Scheme)
	ws := base
	ws.Scheme = wsScheme(u.Scheme)
	ws.Path = strings.TrimSuffix(base.Path, "/") + "/responses"
	return &proxy{baseURL: &base, wsURL: strings.TrimRight(ws.String(), "/"), verbose: verbose, logger: log.Default(), dialer: websocket.Dialer{HandshakeTimeout: 15 * time.Second}, upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}, client: &http.Client{}}, nil
}

func (p *proxy) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"status":"ok","transport":"websocket","routes":"/v1/*"}`)
}

func (p *proxy) responses(w http.ResponseWriter, r *http.Request) {
	if websocket.IsWebSocketUpgrade(r) {
		p.tunnel(w, r)
		return
	}
	if r.Method == http.MethodPost {
		p.httpToWebSocket(w, r)
		return
	}
	p.forwardHTTP(w, r)
}

func (p *proxy) dial(r *http.Request, trace *wsTrace) (*websocket.Conn, *http.Response, error) {
	started := time.Now()
	trace.logf("event=dial_started upstream_host=%q", p.baseURL.Host)
	conn, response, err := p.dialer.DialContext(r.Context(), p.wsURL, forwardedHeaders(r))
	status, requestID := 0, ""
	if response != nil {
		status = response.StatusCode
		requestID = response.Header.Get("X-Oneapi-Request-Id")
		if requestID == "" {
			requestID = response.Header.Get("X-Request-Id")
		}
	}
	trace.logf("event=dial_finished duration_ms=%d status=%d upstream_request_id=%q", time.Since(started).Milliseconds(), status, requestID)
	if err != nil {
		trace.failure("upstream", "handshake", err)
	}
	return conn, response, err
}

func (p *proxy) tunnel(w http.ResponseWriter, r *http.Request) {
	trace := p.newWSTrace("websocket")
	defer trace.finish()
	client, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		trace.failure("client", "handshake", err)
		return
	}
	defer client.Close()
	trace.logf("event=client_connected")
	target, _, err := p.dial(r, trace)
	if err != nil {
		writeWSClose(client, trace, "client", websocket.CloseServiceRestart, "upstream websocket unavailable")
		return
	}
	defer target.Close()
	proxyWebSockets(client, target, trace)
}

func proxyWebSockets(left, right *websocket.Conn, trace *wsTrace) {
	var once sync.Once
	closeBoth := func(side, operation string, err error) {
		once.Do(func() {
			trace.failure(side, operation, err)
			code, reason := wsCloseStatus(err)
			// 先通知另一端，保留合法关闭码；断网等错误不能把保留码 1006 发上线路。
			if side == "client" {
				writeWSClose(right, trace, "upstream", code, reason)
				writeWSClose(left, trace, "client", code, reason)
			} else {
				writeWSClose(left, trace, "client", code, reason)
				writeWSClose(right, trace, "upstream", code, reason)
			}
			_ = left.Close()
			_ = right.Close()
		})
	}
	var wg sync.WaitGroup
	forward := func(dst, src *websocket.Conn, from, to string) {
		for {
			kind, message, err := src.ReadMessage()
			if err != nil {
				closeBoth(from, "read", err)
				return
			}
			trace.received(from, kind, len(message))
			started := time.Now()
			trace.logf("event=write_started side=%s bytes=%d", to, len(message))
			if err := dst.WriteMessage(kind, message); err != nil {
				closeBoth(to, "write", err)
				return
			}
			trace.sent(to, len(message), started)
		}
	}
	wg.Go(func() { forward(right, left, "client", "upstream") })
	wg.Go(func() { forward(left, right, "upstream", "client") })
	wg.Wait()
}

func (p *proxy) httpToWebSocket(w http.ResponseWriter, r *http.Request) {
	trace := p.newWSTrace("http")
	defer trace.finish()
	body, err := io.ReadAll(io.LimitReader(r.Body, 128<<20))
	if err != nil {
		trace.failure("client", "read", err)
		http.Error(w, "failed to read request", http.StatusBadRequest)
		return
	}
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		trace.failure("client", "decode", err)
		http.Error(w, "invalid JSON request", http.StatusBadRequest)
		return
	}
	request["type"] = "response.create"
	message, err := json.Marshal(request)
	if err != nil {
		trace.failure("proxy", "encode", err)
		http.Error(w, "failed to encode request", http.StatusBadRequest)
		return
	}
	trace.received("client", websocket.TextMessage, len(body))
	trace.logf("event=http_request stream=%t", requestBool(request, "stream"))
	target, _, err := p.dial(r, trace)
	if err != nil {
		http.Error(w, fmt.Sprintf("upstream websocket unavailable: %v", err), http.StatusBadGateway)
		return
	}
	defer func() {
		code, reason := trace.closeStatus()
		writeWSClose(target, trace, "upstream", code, reason)
		_ = target.Close()
	}()
	started := time.Now()
	trace.logf("event=write_started side=upstream bytes=%d", len(message))
	if err := target.WriteMessage(websocket.TextMessage, message); err != nil {
		trace.failure("upstream", "write", err)
		http.Error(w, "failed to send request upstream", http.StatusBadGateway)
		return
	}
	trace.sent("upstream", len(message), started)
	if requestBool(request, "stream") {
		p.writeSSE(w, target, trace)
		return
	}
	p.writeJSONResponse(w, target, trace)
}

func (p *proxy) forwardHTTP(w http.ResponseWriter, r *http.Request) {
	target := *p.baseURL
	target.Path = strings.TrimSuffix(p.baseURL.Path, "/") + strings.TrimPrefix(r.URL.Path, "/v1")
	target.RawQuery = r.URL.RawQuery
	request, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	request.Header = forwardedHeaders(r)
	p.logf("HTTP %s %s -> %s", r.Method, r.URL.RequestURI(), target.String())
	response, err := p.client.Do(request)
	if err != nil {
		p.logf("HTTP upstream failed: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	copyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
	p.logf("HTTP response %d for %s %s", response.StatusCode, r.Method, r.URL.Path)
}

func (p *proxy) writeSSE(w http.ResponseWriter, target *websocket.Conn, trace *wsTrace) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher := http.NewResponseController(w)
	for {
		kind, message, err := target.ReadMessage()
		if err != nil {
			trace.failure("upstream", "read", err)
			return
		}
		trace.received("upstream", kind, len(message))
		var event struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(message, &event) != nil {
			continue
		}
		started := time.Now()
		n, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, message)
		if err != nil {
			trace.failure("client", "write", err)
			return
		}
		if err := flusher.Flush(); err != nil {
			trace.failure("client", "flush", err)
			return
		}
		trace.sent("client", n, started)
		if isTerminal(event.Type) {
			trace.logf("event=response_terminal type=%q", event.Type)
			return
		}
	}
}

func (p *proxy) writeJSONResponse(w http.ResponseWriter, target *websocket.Conn, trace *wsTrace) {
	for {
		kind, message, err := target.ReadMessage()
		if err != nil {
			trace.failure("upstream", "read", err)
			http.Error(w, "upstream websocket closed before response", http.StatusBadGateway)
			return
		}
		trace.received("upstream", kind, len(message))
		var event struct {
			Type     string          `json:"type"`
			Response json.RawMessage `json:"response"`
		}
		if json.Unmarshal(message, &event) != nil {
			continue
		}
		if event.Type == "error" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			started := time.Now()
			n, err := w.Write(message)
			if err != nil {
				trace.failure("client", "write", err)
			} else {
				trace.sent("client", n, started)
			}
			trace.logf("event=response_terminal type=%q", event.Type)
			return
		}
		if event.Type == "response.completed" || event.Type == "response.incomplete" || event.Type == "response.failed" {
			w.Header().Set("Content-Type", "application/json")
			payload := message
			if len(event.Response) != 0 {
				payload = event.Response
			}
			started := time.Now()
			n, err := w.Write(payload)
			if err != nil {
				trace.failure("client", "write", err)
			} else {
				trace.sent("client", n, started)
			}
			trace.logf("event=response_terminal type=%q", event.Type)
			return
		}
	}
}

func forwardedHeaders(r *http.Request) http.Header {
	header := make(http.Header)
	for _, name := range []string{"Authorization", "User-Agent", "OpenAI-Organization", "OpenAI-Project", "Content-Type", "Accept", "Accept-Encoding"} {
		if value := r.Header.Get(name); value != "" {
			header.Set(name, value)
		}
	}
	return header
}
func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
func requestBool(values map[string]any, key string) bool {
	value, ok := values[key].(bool)
	return ok && value
}
func isTerminal(kind string) bool {
	return kind == "response.completed" || kind == "response.incomplete" || kind == "response.failed" || kind == "error"
}
func httpScheme(scheme string) string {
	if scheme == "ws" {
		return "http"
	}
	if scheme == "wss" {
		return "https"
	}
	return scheme
}
func wsScheme(scheme string) string {
	if scheme == "http" {
		return "ws"
	}
	if scheme == "https" {
		return "wss"
	}
	return scheme
}
func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
func (p *proxy) logf(format string, args ...any) {
	if p.verbose {
		p.logger.Printf(format, args...)
	}
}

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
}

func installService(listen, target string, verbose bool) error {
	plistPath, err := launchAgentPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return err
	}
	logDir := filepath.Join(filepath.Dir(executable), "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}
	arguments := []string{executable, "-listen", listen, "-target", target}
	if verbose {
		arguments = append(arguments, "-verbose")
	}
	plist := launchdPlist(arguments, filepath.Join(logDir, "codex-ws-proxy.out.log"), filepath.Join(logDir, "codex-ws-proxy.err.log"))
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return err
	}
	_ = runLaunchctl("bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel))
	if err := runLaunchctl("bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), plistPath); err != nil {
		return fmt.Errorf("加载 launchd 服务失败（plist 已写入 %s）: %w", plistPath, err)
	}
	log.Printf("已安装并启动服务 %s", launchdLabel)
	log.Printf("plist: %s", plistPath)
	return nil
}

func uninstallService() error {
	plistPath, err := launchAgentPath()
	if err != nil {
		return err
	}
	_ = runLaunchctl("bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel))
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	log.Printf("已停止并卸载服务 %s", launchdLabel)
	return nil
}

func serviceStatus() error {
	return runLaunchctl("print", fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel))
}

func runLaunchctl(args ...string) error {
	command := exec.Command("launchctl", args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func launchdPlist(arguments []string, stdout, stderr string) string {
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>`)
	builder.WriteString(launchdLabel)
	builder.WriteString(`</string><key>ProgramArguments</key><array>`)
	for _, argument := range arguments {
		builder.WriteString(`<string>`)
		builder.WriteString(xmlEscape(argument))
		builder.WriteString(`</string>`)
	}
	builder.WriteString(`</array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>StandardOutPath</key><string>`)
	builder.WriteString(xmlEscape(stdout))
	builder.WriteString(`</string><key>StandardErrorPath</key><string>`)
	builder.WriteString(xmlEscape(stderr))
	builder.WriteString(`</string></dict></plist>
`)
	return builder.String()
}

func xmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, `"`, "&quot;")
	return strings.ReplaceAll(value, "'", "&apos;")
}
