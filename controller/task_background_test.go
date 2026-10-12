package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/relay"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 同一用例通过真实 SQLite、MySQL、PostgreSQL，覆盖提交、脱离连接、取图及退款。
func TestBackgroundImageLifecycleDatabase(t *testing.T) {
	db, dialect := openTaskDialectDatabase(t, &model.User{}, &model.Channel{}, &model.Task{}, &model.Log{})
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis, oldMemory, oldBatch, oldConsume, oldExport := common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled
	oldLimit := constant.TaskQueryLimit
	constant.TaskQueryLimit = 100
	oldFactory := service.GetTaskAdaptorFunc
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(dialect, dialect)
	common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled = false, false, false, true, false
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor { return relay.GetTaskAdaptor(platform) }
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMain, oldLog)
		common.RedisEnabled, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled = oldRedis, oldMemory, oldBatch, oldConsume, oldExport
		service.GetTaskAdaptorFunc = oldFactory
		constant.TaskQueryLimit = oldLimit
	})
	withTieredBillingConfig(t, map[string]string{"gpt-image-2.5-sunburst": "tiered_expr"}, map[string]string{"gpt-image-2.5-sunburst": `u("images") * 0.01`})
	source, err := plugins.Source("openai-image")
	require.NoError(t, err)
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	for index, mode := range []string{"success", "http_error", "truncated", "timeout", "expired_race"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "timeout" {
				t.Setenv("TASK_BACKGROUND_TIMEOUT_SECONDS", "1")
			}
			gate := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(gate) })
			defer unblock()
			var calls atomic.Int32
			received := make(chan map[string]any, 1)
			imageBytes := strings.Repeat("image", 240000) // 超过旧的 1 MiB JSON 限制。
			encoded := base64.StdEncoding.EncodeToString([]byte(imageBytes))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]any
				if err := common.DecodeJson(r.Body, &body); err != nil {
					http.Error(w, "invalid", 400)
					return
				}
				body["request_path"] = r.URL.Path
				received <- body
				select {
				case <-gate:
				case <-r.Context().Done():
					return
				}
				if mode == "http_error" {
					http.Error(w, "secret upstream details", 500)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if mode == "timeout" {
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				}
				fmt.Fprint(w, ": ping\n\ndata: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"preview\"}\n\n")
				if mode == "truncated" {
					return
				}
				payload, _ := common.Marshal(map[string]any{"type": "image_generation.completed", "b64_json": encoded, "output_format": "png"})
				fmt.Fprintf(w, "event: image_generation.completed\ndata: %s\n\n", payload)
			}))
			defer func() { unblock(); server.Close() }()
			initial := int(20 * common.QuotaPerUnit)
			user := model.User{Username: fmt.Sprintf("bg_user_%d", index), AffCode: fmt.Sprintf("bg_aff_%d", index), Quota: initial}
			require.NoError(t, db.Create(&user).Error)
			ch := model.Channel{Name: "background provider", Type: constant.ChannelTypeTaskPlugin}
			require.NoError(t, db.Create(&ch).Error)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			parent, disconnect := context.WithCancel(context.Background())
			defer disconnect()
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/tasks/openai-image", strings.NewReader(`{}`)).WithContext(parent)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("group", "default")
			c.Set("username", user.Username)
			c.Set("task_request", map[string]any{"model": "gpt-image-2.5-sunburst", "prompt": "一只猫", "n": 1})
			c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Plugin: plugin})
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-image-2.5-sunburst")
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
			common.SetContextKey(c, constant.ContextKeyChannelId, ch.Id)
			common.SetContextKey(c, constant.ContextKeyChannelType, ch.Type)
			info := taskSubmissionRelayInfo(nil)
			info.UserId, info.OriginModelName, info.UserGroup, info.UsingGroup = user.Id, "gpt-image-2.5-sunburst", "default", "default"
			info.IsPlayground = true
			info.UserSetting.BillingPreference = "wallet_only"
			info.PublicTaskID, info.LockedChannel = model.GenerateTaskID(), &ch
			outcome, taskErr := executeTaskSubmission(c, info)
			require.Nil(t, taskErr)
			require.NotNil(t, outcome)
			presentTaskSubmission(c, outcome)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Body.String(), info.PublicTaskID)
			disconnect()
			common.CleanupBodyStorage(c)
			select {
			case body := <-received:
				assert.Equal(t, true, body["stream"])
				assert.Equal(t, "/v1/images/generations", body["request_path"])
				assert.Equal(t, "gpt-image-2.5-sunburst", body["model"])
			case <-time.After(5 * time.Second):
				t.Fatal("未收到上游请求")
			}
			var pending model.Task
			require.NoError(t, db.Where("task_id = ?", info.PublicTaskID).First(&pending).Error)
			assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), pending.Status)
			assert.Positive(t, pending.PrivateData.BackgroundDeadline)
			summary := service.RunTaskPollingOnce(context.Background(), nil)
			assert.Zero(t, summary.PlatformsScanned, "后台任务不可交给上游查询接口")
			assert.Equal(t, 1, summary.UnfinishedTasks)
			if mode == "expired_race" {
				pending.PrivateData.BackgroundDeadline = time.Now().Unix() - 1
				require.NoError(t, db.Model(&pending).Update("private_data", pending.PrivateData).Error)
				service.RunTaskPollingOnce(context.Background(), nil)
			}
			unblock()
			wantQuota := 0
			wantStatus := model.TaskStatus(model.TaskStatusFailure)
			if mode == "success" {
				wantStatus = model.TaskStatusSuccess
				wantQuota = common.QuotaRound(0.01 * common.QuotaPerUnit)
			}
			var stored model.Task
			require.Eventually(t, func() bool {
				if db.Where("task_id = ?", info.PublicTaskID).First(&stored).Error != nil || stored.Status != wantStatus {
					return false
				}
				var updated model.User
				return db.First(&updated, user.Id).Error == nil && updated.Quota == initial-wantQuota && updated.UsedQuota == wantQuota
			}, 10*time.Second, 20*time.Millisecond)
			// 等待工作槽归还，确保异步结算不越过测试数据库的生命周期。
			require.Eventually(t, func() bool {
				releases := []func(){}
				capacity := max(1, common.GetEnvOrDefault("TASK_BACKGROUND_CONCURRENCY", 8))
				for range capacity {
					release, ok := service.AcquireBackgroundTask()
					if !ok {
						break
					}
					releases = append(releases, release)
				}
				count := len(releases)
				for _, release := range releases {
					release()
				}
				return count == capacity
			}, 5*time.Second, 20*time.Millisecond)
			assert.Equal(t, int32(1), calls.Load(), "失败及重启清理不可重发生图请求")
			require.NoError(t, db.Where("task_id = ?", info.PublicTaskID).First(&stored).Error)
			assert.Equal(t, wantQuota, stored.Quota)
			if mode == "success" {
				assert.Contains(t, string(stored.Data), encoded)
				adaptor := relay.GetTaskAdaptor("openai-image")
				adaptor.Init(info)
				artifacts, projectErr := projectTaskArtifacts(&stored)
				require.NoError(t, projectErr)
				require.Len(t, artifacts, 1)
				imageRecorder := httptest.NewRecorder()
				imageContext, _ := gin.CreateTestContext(imageRecorder)
				imageContext.Request = httptest.NewRequest(http.MethodGet, "/image", nil)
				descriptor, contentErr := adaptor.(relaychannel.TaskContentRequestProvider).BuildContentRequest(context.Background(), &stored, "image-1", relaychannel.TaskArtifactClientRequest{Method: http.MethodGet})
				require.NoError(t, contentErr)
				require.NoError(t, proxyTaskMedia(imageContext, &stored, descriptor))
				assert.Equal(t, "image/png", imageRecorder.Header().Get("Content-Type"))
				assert.Equal(t, imageBytes, imageRecorder.Body.String())
			} else {
				assert.NotContains(t, stored.FailReason, "secret")
			}
			var refunds int64
			require.NoError(t, db.Model(&model.Log{}).Where("user_id = ? AND type = ?", user.Id, model.LogTypeRefund).Count(&refunds).Error)
			if mode == "success" {
				assert.Zero(t, refunds)
			} else {
				assert.Equal(t, int64(1), refunds)
			}
			// 重复完成不得重复退款或覆盖已完成结果。
			stale, copyErr := common.DeepCopy(outcome.Task)
			require.NoError(t, copyErr)
			require.NoError(t, service.CompleteBackgroundTask(context.Background(), nil, stale, &relaycommon.TaskInfo{Status: model.TaskStatusFailure, Reason: "duplicate"}, nil, nil))
			var updated model.User
			require.NoError(t, db.First(&updated, user.Id).Error)
			assert.Equal(t, initial-wantQuota, updated.Quota)
		})
	}
}

func TestBackgroundSubmissionInsertFailureDoesNotStartUpstream(t *testing.T) {
	events := []string{}
	setupTaskSubmissionDatabase(t, false, &events)
	billing := &taskSubmissionTestBilling{events: &events}
	c := taskSubmissionTestContext()
	info := taskSubmissionRelayInfo(billing)
	var released, called atomic.Int32
	outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *taskdto.TaskError) {
		return &relay.TaskSubmitResult{Platform: "openai-image", Background: &relay.BackgroundTaskSubmission{
			Release: func() { released.Add(1) },
			Run: func(context.Context) (*relaychannel.TaskSubmitResponse, *taskdto.TaskError) {
				called.Add(1)
				return nil, nil
			},
		}}, nil
	})
	assert.Nil(t, outcome)
	require.NotNil(t, taskErr)
	assert.Equal(t, "task_insert_failed", taskErr.Code)
	assert.Equal(t, int32(0), called.Load())
	assert.Equal(t, int32(1), released.Load())
	assert.Equal(t, 1, billing.refunds)
}
