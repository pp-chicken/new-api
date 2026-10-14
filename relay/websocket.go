package relay

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/wsmanager"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// startClientWSPing 仅发送协议控制帧，不进入业务消息、计费或响应超时统计。
// 返回的停止函数会等待心跳退出；onError 不得调用该停止函数。
func startClientWSPing(ctx context.Context, client *websocket.Conn, disabled bool, onError func(error)) func() {
	if disabled {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	seconds := constant.WebSocketPingIntervalSeconds
	if seconds <= 0 || seconds > 86400 {
		seconds = 10
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Duration(seconds) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				// WriteControl 可与业务写入并发，使用独立的写入期限。
				if err := client.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					onError(err)
					return
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func WssHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	//var requestBody io.Reader
	//firstWssRequest, _ := c.Get("first_wss_request")
	//requestBody = bytes.NewBuffer(firstWssRequest.([]byte))

	statusCodeMappingStr := c.GetString("status_code_mapping")
	resp, err := adaptor.DoRequest(c, info, nil)
	if err != nil {
		return types.NewError(err, types.ErrorCodeDoRequestFailed)
	}

	if resp != nil {
		info.TargetWs = resp.(*websocket.Conn)
		defer info.TargetWs.Close()
		var closeOnce sync.Once
		unregister := wsmanager.Register(info.ChannelId, wsmanager.KindRealtime, func(reason string) {
			closeOnce.Do(func() {
				deadline := time.Now().Add(time.Second)
				closeMessage := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, reason)
				_ = info.ClientWs.WriteControl(websocket.CloseMessage, closeMessage, deadline)
				_ = info.TargetWs.WriteControl(websocket.CloseMessage, closeMessage, deadline)
				_ = info.ClientWs.Close()
				_ = info.TargetWs.Close()
			})
		})
		defer unregister()
	}

	stopPing := startClientWSPing(c.Request.Context(), info.ClientWs, c.GetBool(string(constant.ContextKeyTokenWebSocketPingDisabled)), func(error) {
		_ = info.ClientWs.Close()
		if info.TargetWs != nil {
			_ = info.TargetWs.Close()
		}
	})
	defer stopPing()
	usage, newAPIError := adaptor.DoResponse(c, nil, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}
	service.PostWssConsumeQuota(c, info, info.UpstreamModelName, usage.(*dto.RealtimeUsage), nil)
	return nil
}
