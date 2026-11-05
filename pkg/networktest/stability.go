package networktest

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

const (
	StabilitySubprotocol = "new-api.network-stability.v1"
	StabilityBytes       = 1024
	StabilityInterval    = 5 * time.Second
	MaxStabilityDuration = 24 * time.Hour
	stabilityReady       = "stability-ready 1024 5000"
)

var ErrStabilityAccessDenied = errors.New("稳定性测试访问权限已失效")

func ValidateStabilityDuration(duration time.Duration) error {
	if duration < time.Second || duration > MaxStabilityDuration {
		return fmt.Errorf("持续测试时长必须在 1s～24h 之间")
	}
	return nil
}

// ServeStability 在同一连接上回传少量探测数据；每次回传前重新检查访问权限。
// 调用者负责握手、连接总时长上限和关闭连接。这里不主动发送心跳或自动重连。
func ServeStability(conn *websocket.Conn, timeout time.Duration, checkAccess func() error) error {
	if err := Validate(MinBytes, timeout); err != nil {
		return err
	}
	if checkAccess == nil {
		return ErrStabilityAccessDenied
	}
	if err := phaseDeadline(conn, timeout); err != nil {
		return err
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte(stabilityReady)); err != nil {
		return err
	}
	conn.SetReadLimit(StabilityBytes)
	var lastProbe time.Time
	for sequence := uint64(1); ; sequence++ {
		if err := phaseDeadline(conn, timeout); err != nil {
			return err
		}
		kind, payload, err := conn.ReadMessage()
		if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
			return nil
		}
		if err != nil {
			return err
		}
		if kind != websocket.BinaryMessage || len(payload) != StabilityBytes || binary.BigEndian.Uint64(payload[:8]) != sequence {
			return fmt.Errorf("稳定性探测大小、类型或序号不正确")
		}
		if !lastProbe.IsZero() && time.Since(lastProbe) < StabilityInterval {
			return fmt.Errorf("稳定性探测发送过于频繁，最小间隔为 %s", StabilityInterval)
		}
		if err := checkAccess(); err != nil {
			return ErrStabilityAccessDenied
		}
		lastProbe = time.Now()
		if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
			return err
		}
	}
}

func AwaitStabilityReady(conn *websocket.Conn, timeout time.Duration) error {
	if err := phaseDeadline(conn, timeout); err != nil {
		return err
	}
	return readControl(conn, stabilityReady)
}

type ProbeResult struct {
	RTT   time.Duration
	Stage string
}

// ProbeStability 发送带序号的随机数据，只有回包完全一致才算成功。
// RTT 包含一次完整上传和回传；它不是单向延迟或 TCP 丢包率。
func ProbeStability(conn *websocket.Conn, sequence uint64, timeout time.Duration) (result ProbeResult, err error) {
	result.Stage = "发送探测"
	if err := phaseDeadline(conn, timeout); err != nil {
		return result, err
	}
	payload, started, err := WriteStabilityProbe(conn, sequence, timeout)
	if err != nil {
		return result, err
	}
	result.Stage = "等待回包"
	conn.SetReadLimit(StabilityBytes)
	kind, echo, err := conn.ReadMessage()
	if err != nil {
		return result, err
	}
	result.Stage = "回包校验"
	if err := ValidateStabilityReply(kind, echo, payload); err != nil {
		return result, err
	}
	result.RTT = time.Since(started)
	result.Stage = "完成"
	return result, nil
}

func WriteStabilityProbe(conn *websocket.Conn, sequence uint64, timeout time.Duration) ([]byte, time.Time, error) {
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return nil, time.Time{}, err
	}
	payload := make([]byte, StabilityBytes)
	rand.Read(payload)
	binary.BigEndian.PutUint64(payload[:8], sequence)
	started := time.Now()
	err := conn.WriteMessage(websocket.BinaryMessage, payload)
	return payload, started, err
}

func ValidateStabilityReply(kind int, echo, expected []byte) error {
	if kind != websocket.BinaryMessage || !bytes.Equal(expected, echo) {
		return fmt.Errorf("稳定性探测回包大小、序号或内容不匹配")
	}
	return nil
}
