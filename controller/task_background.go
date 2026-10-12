package controller

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

// startBackgroundTask 只在任务落库、预扣结算及日志完成后启动；不继承客户端取消信号。
func startBackgroundTask(parent context.Context, task *model.Task, submission *relay.BackgroundTaskSubmission) {
	snapshot, err := common.DeepCopy(task)
	if err != nil {
		submission.Release()
		logger.LogError(parent, common.LogText("failed to copy background task"))
		return
	}
	go func() {
		defer submission.Release()
		ctx, cancel := context.WithDeadline(context.WithoutCancel(parent), time.Unix(snapshot.PrivateData.BackgroundDeadline, 0))
		defer cancel()
		result := &relaycommon.TaskInfo{Status: model.TaskStatusFailure, Reason: "Task failed"}
		var data, state []byte
		defer func() {
			if recover() != nil {
				result = &relaycommon.TaskInfo{Status: model.TaskStatusFailure, Reason: "Task failed"}
				data, state = nil, nil
			}
			if result.Status == model.TaskStatusFailure && ctx.Err() == context.DeadlineExceeded {
				result.Reason = "Task timed out"
			}
			// 网络超时仍必须能落库；失败细节不含上游响应或凭证。
			if err := service.CompleteBackgroundTask(context.WithoutCancel(ctx), submission.Adaptor, snapshot, result, data, state); err != nil {
				logger.LogError(context.Background(), common.LogText("failed to persist background task completion: %s", snapshot.TaskID))
			}
		}()
		parsed, taskErr := submission.Run(ctx)
		if taskErr != nil {
			logger.LogWarn(ctx, common.LogText("background task %s failed: %s", snapshot.TaskID, taskErr.Code))
			return
		}
		if parsed == nil || parsed.Immediate == nil {
			return
		}
		if parsed.Immediate.Status != model.TaskStatusSuccess && parsed.Immediate.Status != model.TaskStatusFailure {
			return
		}
		result, data, state = parsed.Immediate, parsed.TaskData, parsed.PluginState
		if result.Status == model.TaskStatusFailure {
			result.Reason = "Task failed"
		}
	}()
}
