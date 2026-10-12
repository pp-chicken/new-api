package jsplugin

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay/helper"
)

// readSubmitEvents owns only SSE framing and bounded JSON state. The plugin
// interprets data, accumulates provider output and decides when it is complete.
// No response bytes are written to the client at this boundary.
func (a *TaskAdaptor) readSubmitEvents(parent context.Context, resp *http.Response, driverContext map[string]any) (_ any, readErr error) {
	started := time.Now()
	stage, media := "content_type", "other"
	lines, events, bytesRead := 0, 0, 0
	lastEvent := "none"
	// 只记录阶段、计数及固定分类，不记录原始错误、事件数据、请求体或响应头。
	defer func() {
		if a.plugin.Meta.SubmitMode != "background" {
			return
		}
		reason := "none"
		if readErr != nil {
			reason = "stage_failed"
			switch {
			case errors.Is(readErr, context.DeadlineExceeded):
				reason = "deadline_exceeded"
			case errors.Is(readErr, context.Canceled):
				reason = "context_canceled"
			case readErr.Error() == "task submit stream idle timeout":
				reason = "idle_timeout"
			}
			if stage == "plugin_hook" {
				// 仅将本插件的固定校验文案映射为代码，绝不输出未知异常内容。
				for _, item := range []struct{ text, code string }{
					{"图片流事件不是有效 JSON", "invalid_json"},
					{"图片流事件必须是对象", "invalid_event_shape"},
					{"上游生图失败", "upstream_error_event"},
					{"完成事件缺少图片内容", "missing_image_payload"},
					{"不支持的图片格式", "unsupported_image_format"},
					{"图片流未返回全部完整图片", "incomplete_images"},
					{"图片数量超过请求数量", "excess_images"},
				} {
					if strings.Contains(readErr.Error(), item.text) {
						reason = item.code
						break
					}
				}
			}
		}
		message := fmt.Sprintf("background_image_stream task=%s plugin=%s status=%d media=%s stage=%s reason=%s lines=%d events=%d bytes=%d last_event=%s elapsed_ms=%d",
			driverContext["publicTaskId"], a.plugin.Meta.Key, resp.StatusCode, media, stage, reason, lines, events, bytesRead, lastEvent, time.Since(started).Milliseconds())
		if readErr != nil {
			logger.LogWarn(parent, message)
		} else {
			logger.LogInfo(parent, message)
		}
	}()
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	switch mediaType {
	case "text/event-stream", "application/json", "text/html", "text/plain":
		media = mediaType
	}
	if err != nil || mediaType != "text/event-stream" {
		return nil, fmt.Errorf("expected a text/event-stream submit response")
	}
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	stopClose := context.AfterFunc(ctx, func() { _ = resp.Body.Close() })
	defer stopClose()
	idleTimeout := time.Duration(constant.StreamingTimeout) * time.Second
	if idleTimeout <= 0 {
		idleTimeout = 30 * time.Second
	}
	timer := time.AfterFunc(idleTimeout, func() { cancel(fmt.Errorf("task submit stream idle timeout")) })
	defer timer.Stop()

	scanner := helper.NewStreamScanner(resp.Body, a.resultLimit()+1)
	hook := "parseSubmitEvent"
	var accumulated *pluginruntime.JSONState
	if pluginruntime.UsesSubmitEventDelta(a.plugin.Meta, a.plugin.Engine) {
		hook = "parseSubmitEventDelta"
		accumulated = pluginruntime.NewJSONState(a.resultLimit())
	}
	var state any
	var data []string
	var eventName, lastID string
	frameBytes := 0
	firstLine := true
	stage = "read"
	for scanner.Scan() {
		if err := context.Cause(ctx); err != nil {
			return nil, err
		}
		timer.Reset(idleTimeout)
		line := scanner.Text()
		lines++
		bytesRead += len(line) + 1
		if firstLine {
			line = strings.TrimPrefix(line, "\ufeff")
			firstLine = false
		}
		frameBytes += len(line) + 1
		if frameBytes > a.resultLimit() {
			stage = "event_size"
			return nil, fmt.Errorf("task submit SSE event exceeds size limit")
		}
		if line != "" {
			field, value, _ := strings.Cut(line, ":")
			value, _ = strings.CutPrefix(value, " ")
			switch field {
			case "data":
				data = append(data, value)
			case "event":
				eventName = value
			case "id":
				if !strings.ContainsRune(value, '\x00') {
					lastID = value
				}
			}
			continue
		}
		frameBytes = 0
		if len(data) == 0 {
			eventName = ""
			continue
		}
		if eventName == "" {
			eventName = "message"
		}
		event := map[string]any{"event": eventName, "id": lastID, "data": strings.Join(data, "\n")}
		events++
		lastEvent = "other"
		switch eventName {
		case "message", "image_generation.completed", "image_generation.partial_image", "error":
			lastEvent = eventName
		}
		stage = "plugin_hook"
		value, err := a.plugin.Engine.Call(ctx, hook, driverContext, event, state)
		if err != nil {
			return nil, err
		}
		stage = "hook_result"
		result, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must return an object", hook)
		}
		nextState, hasState := result["state"]
		done, hasDone := result["done"].(bool)
		if !hasState || !hasDone {
			return nil, fmt.Errorf("%s must return state and a boolean done", hook)
		}
		encoded, err := common.Marshal(nextState)
		if err != nil {
			return nil, fmt.Errorf("invalid submit stream state: %w", err)
		}
		if len(encoded) > maxTaskPluginPersistedJSONBytes {
			return nil, fmt.Errorf("task submit stream state exceeds size limit")
		}
		if accumulated != nil {
			stage = "accumulation"
			if len(result) != 3 {
				return nil, fmt.Errorf("parseSubmitEventDelta must return only changes, state and done")
			}
			// Control state is separate from the full result and is the only
			// snapshot passed back to JS on the next event.
			if len(encoded) > 64<<10 {
				return nil, fmt.Errorf("task submit stream control state exceeds size limit")
			}
			if err = accumulated.Apply(ctx, result["changes"]); err != nil {
				return nil, fmt.Errorf("invalid submit stream changes: %w", err)
			}
		}
		// Keep the exact encoded-byte limit above. Plain JSON state goes back
		// to the next event as is; codec-specific values (for example exported
		// typed arrays) retain the old normalization.
		if isPlainJSONValue(nextState, 0) {
			state = nextState
		} else {
			var decoded any
			if err = common.Unmarshal(encoded, &decoded); err != nil {
				return nil, err
			}
			state = decoded
		}
		if done {
			stage = "complete"
			if accumulated != nil {
				return accumulated.Value()
			}
			return state, nil
		}
		data = nil
		eventName = ""
		stage = "read"
	}
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read task submit stream: %w", err)
	}
	stage = "eof_before_completion"
	return nil, fmt.Errorf("task submit stream ended before the plugin reported completion")
}
