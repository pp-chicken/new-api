package controller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
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
	protocols := websocket.Subprotocols(c.Request)
	stability := slices.Contains(protocols, networktest.StabilitySubprotocol)
	subprotocol := networktest.Subprotocol
	size := networktest.StabilityBytes
	lifetime := networktest.MaxDuration
	if stability {
		milliseconds, err := strconv.ParseInt(c.Query("duration_ms"), 10, 64)
		if err != nil || milliseconds < 1000 || milliseconds > networktest.MaxStabilityDuration.Milliseconds() {
			c.String(http.StatusBadRequest, "稳定性测试时长必须在 1s～24h 之间")
			return
		}
		subprotocol = networktest.StabilitySubprotocol
		// 给客户端正常收尾预留时间；无探测数据仍受每次读取的超时约束。
		lifetime = time.Duration(milliseconds)*time.Millisecond + networktest.MaxPhaseTimeout + 10*time.Second
	} else {
		var err error
		size, err = strconv.Atoi(c.Query("bytes"))
		if err != nil || networktest.Validate(size, networktest.MaxPhaseTimeout) != nil {
			c.String(http.StatusBadRequest, "测试大小必须在 65536～33554432 字节之间")
			return
		}
	}
	if !slices.Contains(protocols, subprotocol) {
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
	upgrader := websocket.Upgrader{Subprotocols: []string{subprotocol}, HandshakeTimeout: 10 * time.Second}
	// Hijack 后不会继承 Gin Writer 上的响应头，必须显式保留请求 ID。
	headers := http.Header{common.RequestIdKey: {c.GetString(common.RequestIdKey)}}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, headers)
	if err != nil {
		return
	}
	defer conn.Close()
	stop := context.AfterFunc(c.Request.Context(), func() { _ = conn.Close() })
	defer stop()
	timer := time.AfterFunc(lifetime, func() { _ = conn.Close() })
	defer timer.Stop()
	started := time.Now()
	if stability {
		tokenKey, tokenID := c.GetString("token_key"), c.GetInt("token_id")
		clientIP := net.ParseIP(c.ClientIP())
		err = networktest.ServeStability(conn, networktest.MaxPhaseTimeout, func() error {
			token, err := model.ValidateUserToken(tokenKey)
			if err != nil || token == nil || token.Id != tokenID || token.UserId != userID {
				return networktest.ErrStabilityAccessDenied
			}
			if allowed := token.GetIpLimits(); len(allowed) > 0 && (clientIP == nil || !common.IsIpInCIDRList(clientIP, allowed)) {
				return networktest.ErrStabilityAccessDenied
			}
			user, err := model.GetUserCache(userID)
			if err != nil || user.Status != common.UserStatusEnabled {
				return networktest.ErrStabilityAccessDenied
			}
			return nil
		})
	} else {
		err = networktest.Serve(conn, size, networktest.MaxPhaseTimeout)
	}
	if err != nil {
		reason := "测速数据不完整、协议错误或超时"
		if errors.Is(err, networktest.ErrStabilityAccessDenied) {
			reason = "测试令牌已失效或访问被拒绝"
		}
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, reason), time.Now().Add(time.Second))
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("网络测试结束 user_id=%d protocol=%s bytes_per_sample=%d success=%t duration_ms=%d", userID, subprotocol, size, err == nil, time.Since(started).Milliseconds()))
}
