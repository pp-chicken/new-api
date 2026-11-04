// Package networktest 实现一次有大小和时限约束的 WebSocket 双向测速。
package networktest

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"time"

	"github.com/gorilla/websocket"
)

const (
	Path            = "/network-test"
	Subprotocol     = "new-api.network-test.v1"
	DefaultBytes    = 1 << 20
	MinBytes        = 64 << 10
	MaxBytes        = 32 << 20
	DefaultTimeout  = 60 * time.Second
	MaxPhaseTimeout = 120 * time.Second
	MaxDuration     = 2*MaxPhaseTimeout + 30*time.Second
)

type Result struct {
	Bytes       int
	RTT         time.Duration
	UploadWrite time.Duration
	Upload      time.Duration
	Download    time.Duration
}

func Validate(size int, timeout time.Duration) error {
	if size < MinBytes || size > MaxBytes {
		return fmt.Errorf("每个方向的测试字节数必须在 %d～%d 之间", MinBytes, MaxBytes)
	}
	if timeout <= 0 || timeout > MaxPhaseTimeout {
		return fmt.Errorf("每阶段超时必须大于 0 且不超过 %s", MaxPhaseTimeout)
	}
	return nil
}

// Serve 只接收一轮上传并生成一轮下载；调用者负责认证、限流、Upgrade 和关闭连接。
// 文本控制帧最多 128 字节，数据使用单条二进制消息，流式处理而不保留正文。
func Serve(conn *websocket.Conn, size int, timeout time.Duration) error {
	if err := Validate(size, timeout); err != nil {
		return err
	}
	if err := phaseDeadline(conn, min(timeout, 10*time.Second)); err != nil {
		return err
	}
	if err := conn.WriteMessage(websocket.TextMessage, fmt.Appendf(nil, "ready %d", size)); err != nil {
		return err
	}
	if err := readControl(conn, "ping"); err != nil {
		return err
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("pong")); err != nil {
		return err
	}
	if err := phaseDeadline(conn, timeout); err != nil {
		return err
	}
	digest, err := readPayload(conn, size)
	if err != nil {
		return fmt.Errorf("接收上传失败: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("uploaded "+digest)); err != nil {
		return err
	}
	if err := phaseDeadline(conn, min(timeout, 10*time.Second)); err != nil {
		return err
	}
	if err := readControl(conn, "download"); err != nil {
		return err
	}
	if err := phaseDeadline(conn, timeout); err != nil {
		return err
	}
	digest, err = writePayload(conn, size)
	if err != nil {
		return fmt.Errorf("发送下载失败: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("downloaded "+digest)); err != nil {
		return err
	}
	return conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "测速完成"), time.Now().Add(time.Second))
}

// Run 等待上传确认、验证两个方向的 SHA-256；失败时保留已完成阶段的结果。
// 上传包含服务端确认往返，下载包含发起请求到收齐数据和摘要的耗时。
func Run(conn *websocket.Conn, size int, timeout time.Duration) (result Result, err error) {
	result.Bytes = size
	if err := Validate(size, timeout); err != nil {
		return result, err
	}
	if err := phaseDeadline(conn, timeout); err != nil {
		return result, err
	}
	if err := readControl(conn, fmt.Sprintf("ready %d", size)); err != nil {
		return result, fmt.Errorf("服务端准备阶段失败: %w", err)
	}
	started := time.Now()
	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		return result, fmt.Errorf("往返测试发送失败: %w", err)
	}
	if err := readControl(conn, "pong"); err != nil {
		return result, fmt.Errorf("往返测试接收失败: %w", err)
	}
	result.RTT = time.Since(started)
	if err := phaseDeadline(conn, timeout); err != nil {
		return result, err
	}
	started = time.Now()
	digest, err := writePayload(conn, size)
	if err != nil {
		return result, fmt.Errorf("上传写入失败（已用 %s）: %w", time.Since(started).Round(time.Millisecond), err)
	}
	result.UploadWrite = time.Since(started)
	if err := readControl(conn, "uploaded "+digest); err != nil {
		return result, fmt.Errorf("上传确认或校验失败（已用 %s）: %w", time.Since(started).Round(time.Millisecond), err)
	}
	result.Upload = time.Since(started)
	if err := phaseDeadline(conn, timeout); err != nil {
		return result, err
	}
	started = time.Now()
	if err := conn.WriteMessage(websocket.TextMessage, []byte("download")); err != nil {
		return result, fmt.Errorf("下载请求失败: %w", err)
	}
	digest, err = readPayload(conn, size)
	if err != nil {
		return result, fmt.Errorf("下载接收失败（已用 %s）: %w", time.Since(started).Round(time.Millisecond), err)
	}
	if err := readControl(conn, "downloaded "+digest); err != nil {
		return result, fmt.Errorf("下载校验失败: %w", err)
	}
	result.Download = time.Since(started)
	return result, nil
}

func phaseDeadline(conn *websocket.Conn, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	if err := conn.SetReadDeadline(deadline); err != nil {
		return err
	}
	return conn.SetWriteDeadline(deadline)
}

func readControl(conn *websocket.Conn, expected string) error {
	conn.SetReadLimit(128)
	kind, payload, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	if kind != websocket.TextMessage || string(payload) != expected {
		return fmt.Errorf("测速控制消息或摘要不匹配")
	}
	return nil
}

func readPayload(conn *websocket.Conn, size int) (string, error) {
	conn.SetReadLimit(int64(size))
	kind, reader, err := conn.NextReader()
	if err != nil {
		return "", err
	}
	if kind != websocket.BinaryMessage {
		return "", fmt.Errorf("测速数据必须是二进制消息")
	}
	hash := sha256.New()
	count, err := io.Copy(hash, reader)
	if err != nil {
		return "", err
	}
	if count != int64(size) {
		return "", fmt.Errorf("数据不完整: 收到 %d 字节，预期 %d 字节", count, size)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func writePayload(conn *websocket.Conn, size int) (string, error) {
	writer, err := conn.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(writer, hash), rand.Reader, int64(size)); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
