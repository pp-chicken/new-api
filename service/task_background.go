package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

var backgroundTaskSlots = make(chan struct{}, max(1, common.GetEnvOrDefault("TASK_BACKGROUND_CONCURRENCY", 8)))

// AcquireBackgroundTask 限制后台任务数量；容量不足由提交编排退还预扣，不积累无界队列。
func AcquireBackgroundTask() (func(), bool) {
	select {
	case backgroundTaskSlots <- struct{}{}:
		return sync.OnceFunc(func() { <-backgroundTaskSlots }), true
	default:
		return nil, false
	}
}

func BackgroundTaskTimeout() time.Duration {
	return time.Duration(max(1, common.GetEnvOrDefault("TASK_BACKGROUND_TIMEOUT_SECONDS", 900))) * time.Second
}

// CompleteBackgroundTask 与轮询共用终态 CAS 和结算；只有获胜者能退款或调整额度。
func CompleteBackgroundTask(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, result *relaycommon.TaskInfo, data, state []byte) error {
	if result == nil || (result.Status != model.TaskStatusSuccess && result.Status != model.TaskStatusFailure) {
		return fmt.Errorf("background submission requires a terminal result")
	}
	if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
		return nil
	}
	previous := task.Status
	task.Status = model.TaskStatus(result.Status)
	task.Progress = "100%"
	task.FinishTime = time.Now().Unix()
	task.Data = data
	task.PrivateData.PluginState = state
	if task.Status == model.TaskStatusFailure {
		task.FailReason = result.Reason
	}
	won, err := task.UpdateWithStatus(previous)
	if err != nil {
		return err
	}
	if won {
		finalizeTerminalTask(ctx, adaptor, task, result)
	}
	return nil
}
