package main

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type wsTraffic struct {
	readMessages    int64
	readBytes       int64
	writtenMessages int64
	writtenBytes    int64
	firstRead       time.Time
	lastRead        time.Time
}

// wsTrace 只记录连接、消息计数与时序，不保存正文或请求头。
type wsTrace struct {
	proxy          *proxy
	id             string
	started        time.Time
	mu             sync.Mutex
	client         wsTraffic
	upstream       wsTraffic
	firstError     error
	closeSource    string
	closeOperation string
}

func (p *proxy) newWSTrace(mode string) *wsTrace {
	trace := &wsTrace{proxy: p, started: time.Now()}
	trace.id = fmt.Sprintf("%d-%d-%d", trace.started.UnixMilli(), os.Getpid(), p.sequence.Add(1))
	trace.logf("event=connection_started mode=%s", mode)
	return trace
}

func (t *wsTrace) logf(format string, args ...any) {
	if !t.proxy.verbose {
		return
	}
	values := append([]any{t.id, time.Since(t.started).Milliseconds()}, args...)
	t.proxy.logf("ws_id=%s elapsed_ms=%d "+format, values...)
}

func (t *wsTrace) received(side string, kind, size int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	stats := &t.client
	if side == "upstream" {
		stats = &t.upstream
	}
	now := time.Now()
	gap := int64(0)
	if stats.firstRead.IsZero() {
		stats.firstRead = now
		t.logf("event=first_message side=%s at=%q", side, now.Format(time.RFC3339Nano))
	} else {
		gap = now.Sub(stats.lastRead).Milliseconds()
	}
	stats.readMessages++
	stats.readBytes += int64(size)
	stats.lastRead = now
	t.logf("event=message_received side=%s message=%d opcode=%d bytes=%d gap_ms=%d", side, stats.readMessages, kind, size, gap)
}

func (t *wsTrace) sent(side string, size int, started time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	stats := &t.client
	if side == "upstream" {
		stats = &t.upstream
	}
	stats.writtenMessages++
	stats.writtenBytes += int64(size)
	t.logf("event=message_sent side=%s message=%d bytes=%d write_ms=%d", side, stats.writtenMessages, size, time.Since(started).Milliseconds())
}

func (t *wsTrace) failure(side, operation string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.firstError != nil {
		return
	}
	t.firstError, t.closeSource, t.closeOperation = err, side, operation
	code, reason := 0, ""
	var closeError *websocket.CloseError
	if errors.As(err, &closeError) {
		code, reason = closeError.Code, closeError.Text
	}
	t.logf("event=connection_closing source=%s operation=%s original_close_code=%d close_reason=%q error=%q", side, operation, code, reason, err)
}

func (t *wsTrace) closeStatus() (int, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.firstError == nil {
		return websocket.CloseNormalClosure, "HTTP response complete"
	}
	return wsCloseStatus(t.firstError)
}

func (t *wsTrace) finish() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logf("event=connection_finished source=%q operation=%q client_messages_in=%d client_bytes_in=%d client_messages_out=%d client_bytes_out=%d upstream_messages_in=%d upstream_bytes_in=%d upstream_messages_out=%d upstream_bytes_out=%d",
		t.closeSource, t.closeOperation, t.client.readMessages, t.client.readBytes, t.client.writtenMessages, t.client.writtenBytes,
		t.upstream.readMessages, t.upstream.readBytes, t.upstream.writtenMessages, t.upstream.writtenBytes)
}

func wsCloseStatus(err error) (int, string) {
	var closeError *websocket.CloseError
	if errors.As(err, &closeError) {
		code := closeError.Code
		switch code {
		case websocket.CloseNoStatusReceived:
			// 空 Close 帧可以原样传递，1005 本身不写入帧。
			return code, ""
		case websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseProtocolError,
			websocket.CloseUnsupportedData, websocket.CloseInvalidFramePayloadData,
			websocket.ClosePolicyViolation, websocket.CloseMessageTooBig, websocket.CloseMandatoryExtension,
			websocket.CloseInternalServerErr, websocket.CloseServiceRestart, websocket.CloseTryAgainLater, 1014:
			return code, closeError.Text
		}
		if code >= 3000 && code <= 4999 {
			return code, closeError.Text
		}
	}
	return websocket.CloseInternalServerErr, "proxy transport error"
}

func writeWSClose(conn *websocket.Conn, trace *wsTrace, side string, code int, reason string) {
	err := conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
	if errors.Is(err, websocket.ErrCloseSent) {
		return
	}
	if err != nil {
		trace.logf("event=close_write_failed side=%s close_code=%d error=%q", side, code, err)
		return
	}
	trace.logf("event=close_sent side=%s close_code=%d close_reason=%q", side, code, reason)
}
