package controller

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/networktest"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var networkTestConnections = struct {
	sync.Mutex
	users map[int]bool
}{users: make(map[int]bool)}

// NetworkTest 必须放在 TokenAuth 和 NetworkTestRateLimit 之后，不经过模型分发。
func NetworkTest(c *gin.Context) {
	size, err := strconv.Atoi(c.Query("bytes"))
	if err != nil || networktest.Validate(size, networktest.MaxPhaseTimeout) != nil {
		c.String(http.StatusBadRequest, "测试大小必须在 65536～33554432 字节之间")
		return
	}
	if !slices.Contains(websocket.Subprotocols(c.Request), networktest.Subprotocol) {
		c.String(http.StatusBadRequest, "缺少支持的测速子协议")
		return
	}
	userID := c.GetInt("id")
	if userID == 0 {
		c.Status(http.StatusUnauthorized)
		return
	}
	// 单实例最多 8 条测速连接，同一用户在该实例最多 1 条。
	networkTestConnections.Lock()
	busy := networkTestConnections.users[userID] || len(networkTestConnections.users) >= 8
	if !busy {
		networkTestConnections.users[userID] = true
	}
	networkTestConnections.Unlock()
	if busy {
		c.Header("Retry-After", "5")
		c.Status(http.StatusTooManyRequests)
		return
	}
	defer func() {
		networkTestConnections.Lock()
		delete(networkTestConnections.users, userID)
		networkTestConnections.Unlock()
	}()
	upgrader := websocket.Upgrader{Subprotocols: []string{networktest.Subprotocol}, HandshakeTimeout: 10 * time.Second}
	// Hijack 后不会继承 Gin Writer 上的响应头，必须显式保留请求 ID。
	headers := http.Header{common.RequestIdKey: {c.GetString(common.RequestIdKey)}}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, headers)
	if err != nil {
		return
	}
	defer conn.Close()
	stop := context.AfterFunc(c.Request.Context(), func() { _ = conn.Close() })
	defer stop()
	timer := time.AfterFunc(networktest.MaxDuration, func() { _ = conn.Close() })
	defer timer.Stop()
	started := time.Now()
	err = networktest.Serve(conn, size, networktest.MaxPhaseTimeout)
	if err != nil {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "测速数据不完整、协议错误或超时"), time.Now().Add(time.Second))
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("网络测速结束 user_id=%d bytes_per_direction=%d success=%t duration_ms=%d", userID, size, err == nil, time.Since(started).Milliseconds()))
}
