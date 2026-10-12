package jsplugin

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackgroundImageDiagnostics(t *testing.T) {
	var output bytes.Buffer
	common.LogWriterMu.Lock()
	previous := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previous
		common.LogWriterMu.Unlock()
	})
	source, err := plugins.Source("openai-image")
	require.NoError(t, err)
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{Log: func(message string) { output.WriteString(message) }})
	require.NoError(t, err)
	for _, tc := range []struct{ name, media, body, diagnostic string }{
		{"非流响应", "application/json", `{"secret":"DO_NOT_LOG"}`, "stage=content_type"},
		{"提前结束", "text/event-stream", "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"DO_NOT_LOG\"}\n\n", "stage=eof_before_completion"},
		{"错误事件", "text/event-stream", "data: {\"error\":{\"message\":\"DO_NOT_LOG\"}}\n\n", "reason=upstream_error_event"},
		{"无效JSON", "text/event-stream", "data: DO_NOT_LOG\n\n", "reason=invalid_json"},
		{"缺少图片", "text/event-stream", "data: {\"type\":\"image_generation.completed\",\"revised_prompt\":\"DO_NOT_LOG\"}\n\n", "reason=missing_image_payload"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output.Reset()
			response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{tc.media}}, Body: io.NopCloser(strings.NewReader(tc.body))}
			_, err := New(plugin).readSubmitEvents(t.Context(), response, map[string]any{"publicTaskId": "task_diagnostic", "requestBody": map[string]any{"n": 1}})
			require.Error(t, err)
			assert.Contains(t, output.String(), "task=task_diagnostic")
			assert.Contains(t, output.String(), tc.diagnostic)
			assert.NotContains(t, output.String(), "DO_NOT_LOG")
		})
	}
}

func TestBackgroundImageModelNames(t *testing.T) {
	source, err := plugins.Source("openai-image")
	require.NoError(t, err)
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	for _, tc := range []struct{ model, upstream string }{
		{"gpt-image-1.5", ""},
		{"gpt-image-2", ""},
		{"gpt-image-2.5", ""},
		{"gpt-image-2.5-flare", ""},
		{"gpt-image-2.5-sunburst", ""},
		{"my-custom-image", ""},
		{"my-image-alias", "gpt-image-2.5-flare"},
		{"my-deployment-alias", "vendor/custom-image-v3"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			adaptor := New(plugin)
			info := &relaycommon.RelayInfo{
				ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://example.com", UpstreamModelName: tc.upstream},
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_model_test"},
				OriginModelName: tc.model,
			}
			adaptor.Init(info)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/tasks/openai-image", nil)
			c.Set("task_request", map[string]any{"model": tc.model, "prompt": "一只猫", "stream": false})
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			body, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			var submitted map[string]any
			require.NoError(t, common.DecodeJson(body, &submitted))
			want := tc.upstream
			if want == "" {
				want = tc.model
			}
			assert.Equal(t, want, submitted["model"])
			assert.Equal(t, true, submitted["stream"])
			assert.Equal(t, tc.model, info.OriginModelName)
		})
	}
}

func TestBackgroundImageStreamContract(t *testing.T) {
	source, err := plugins.Source("openai-image")
	require.NoError(t, err)
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, stream string
		n            int
		ok           bool
	}{
		{"完成事件无需DONE", "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"YQ==\"}\n\n", 1, true},
		{"多张图片", "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"YQ==\"}\n\ndata: {\"type\":\"image_generation.completed\",\"url\":\"https://example.com/2.png\"}\n\n", 2, true},
		{"上游错误", "data: {\"type\":\"error\",\"error\":{\"message\":\"failure\"}}\n\n", 1, false},
		{"只有预览", "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"YQ==\"}\n\ndata: [DONE]\n\n", 1, false},
		{"少于请求张数", "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"YQ==\"}\n\ndata: [DONE]\n\n", 2, false},
		{"意外EOF", "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"YQ==\"}\n\n", 1, false},
		{"空完成事件", "data: {\"type\":\"image_generation.completed\"}\n\n", 1, false},
		{"错误格式", "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"YQ==\",\"output_format\":\"svg\"}\n\n", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adaptor := New(plugin)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://example.com"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}, OriginModelName: "gpt-image-2.5-sunburst"}
			adaptor.Init(info)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/tasks/openai-image", nil)
			c.Set("task_request", map[string]any{"model": info.OriginModelName, "prompt": "一只猫", "n": tc.n})
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(tc.stream))}
			parsed, taskErr := adaptor.ParseResponse(c, response, info)
			if !tc.ok {
				require.NotNil(t, taskErr)
				assert.True(t, taskErr.NoRetry)
				return
			}
			require.Nil(t, taskErr)
			require.Equal(t, model.TaskStatusSuccess, parsed.Immediate.Status)
			var result struct {
				Data []any `json:"data"`
			}
			require.NoError(t, common.Unmarshal(parsed.TaskData, &result))
			assert.Len(t, result.Data, tc.n)
			assert.False(t, c.Writer.Written())
		})
	}
}

func TestBackgroundImageInlineArtifacts(t *testing.T) {
	source, err := plugins.Source("openai-image")
	require.NoError(t, err)
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://example.com"}})
	for _, tc := range []struct {
		mime, payload string
		ok            bool
	}{
		{"image/png", "YQ==", true}, {"image/jpeg", "YQ==", true}, {"image/webp", "YQ==", true},
		{"image/svg+xml", "YQ==", false}, {"text/html", "YQ==", false}, {"image/png", "!invalid!", false},
	} {
		t.Run(tc.mime+tc.payload, func(t *testing.T) {
			task := &model.Task{TaskID: "task_public", Status: model.TaskStatusSuccess}
			task.SetData(map[string]any{"data": []any{map[string]any{"b64_json": tc.payload, "mime_type": tc.mime}}})
			descriptor, err := adaptor.BuildContentRequest(t.Context(), task, "image-1", channel.TaskArtifactClientRequest{Method: http.MethodGet})
			if tc.ok {
				require.NoError(t, err)
				assert.True(t, descriptor.Credentialless)
			} else {
				require.Error(t, err)
			}
		})
	}
}
