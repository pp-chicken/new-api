package jsplugin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackgroundImageEditing(t *testing.T) {
	source, err := plugins.Source("openai-image")
	require.NoError(t, err)
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	// 输入图片数组不能作为计费用量；输出张数事实仍受数字类型约束。
	_, _, err = New(plugin).validatedUsageRatios(map[string]any{"images": []any{map[string]any{"file_id": "file-input"}}})
	require.Error(t, err)
	for _, tc := range []struct {
		name, fields, event    string
		invalid, streamFailure bool
	}{
		{name: "自动识别URL编辑", fields: `"images":[{"image_url":"https://example.com/input.png"}]`},
		{name: "多图与遮罩", fields: `"action":"image_to_image","images":[{"image_url":"data:image/png;base64,YQ=="},{"file_id":"file-input"}],"mask":{"image_url":"https://example.com/mask.png"},"n":2`},
		{name: "编辑别名", fields: `"action":"edit","images":[{"file_id":"file-input"}],"mask":{"file_id":"file-mask"}`},
		{name: "事件名称回退", fields: `"images":[{"file_id":"file-input"}]`, event: "event: image_edit.completed\ndata: {\"b64_json\":\"YQ==\"}\n\n"},
		{name: "拒绝文生图完成事件", fields: `"images":[{"file_id":"file-input"}]`, event: "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"YQ==\"}\n\n", streamFailure: true},
		{name: "编辑预览不能算完成", fields: `"images":[{"file_id":"file-input"}]`, event: "data: {\"type\":\"image_edit.partial_image\",\"b64_json\":\"YQ==\"}\n\ndata: [DONE]\n\n", streamFailure: true},
		{name: "编辑上游错误", fields: `"images":[{"file_id":"file-input"}]`, event: "data: {\"type\":\"error\",\"error\":{\"message\":\"failed\"}}\n\n", streamFailure: true},
		{name: "缺少输入", fields: `"action":"edit"`, invalid: true},
		{name: "空输入", fields: `"images":[]`, invalid: true},
		{name: "输入类型错误", fields: `"images":"https://example.com/a.png"`, invalid: true},
		{name: "引用冲突", fields: `"images":[{"image_url":"https://example.com/a.png","file_id":"file-a"}]`, invalid: true},
		{name: "空文件ID", fields: `"images":[{"file_id":" "}]`, invalid: true},
		{name: "本地路径无效", fields: `"images":[{"image_url":"/tmp/a.png"}]`, invalid: true},
		{name: "无效遮罩", fields: `"images":[{"file_id":"file-a"}],"mask":{}`, invalid: true},
		{name: "动作冲突", fields: `"action":"text_to_image","images":[{"file_id":"file-a"}]`, invalid: true},
		{name: "拒绝未知动作", fields: `"action":"unknown"`, invalid: true},
		{name: "拒绝文件字段", fields: `"image":"a.png"`, invalid: true},
		{name: "拒绝负输出张数", fields: `"images":[{"file_id":"file-a"}],"n":-1`, invalid: true},
		{name: "拒绝过多输出张数", fields: `"images":[{"file_id":"file-a"}],"n":11`, invalid: true},
		{name: "过多输入", fields: `"images":[` + strings.TrimSuffix(strings.Repeat(`{"file_id":"file-a"},`, 17), ",") + `]`, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request map[string]any
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"image-alias","prompt":"把背景换成蓝色","stream":false,`+tc.fields+`}`, &request))
			adaptor := New(plugin)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://example.com/", UpstreamModelName: "gpt-image-1.5"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_edit"}, OriginModelName: "image-alias"}
			adaptor.Init(info)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/tasks/openai-image", nil)
			c.Set("task_request", request)
			taskErr := adaptor.ValidateRequestAndSetAction(c, info)
			if tc.invalid {
				require.NotNil(t, taskErr)
				return
			}
			require.Nil(t, taskErr)
			assert.Equal(t, "image_to_image", info.Action)
			assert.Equal(t, "https://example.com/v1/images/edits", adaptor.submit.URL)
			body, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			var submitted map[string]any
			require.NoError(t, common.DecodeJson(body, &submitted))
			assert.Equal(t, request["images"], submitted["images"])
			assert.Equal(t, request["mask"], submitted["mask"])
			assert.Equal(t, "gpt-image-1.5", submitted["model"])
			assert.Equal(t, true, submitted["stream"])
			assert.NotContains(t, submitted, "action")
			assert.Equal(t, false, request["stream"])
			count := 1
			if n, ok := request["n"].(float64); ok {
				count = int(n)
			}
			stream := tc.event
			if stream == "" {
				stream = "data: {\"type\":\"image_edit.partial_image\",\"b64_json\":\"PREVIEW\"}\n\n" + strings.Repeat("data: {\"type\":\"image_edit.completed\",\"b64_json\":\"YQ==\",\"output_format\":\"webp\"}\n\n", count)
			}
			response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}
			result, taskErr := adaptor.ParseResponse(c, response, info)
			if tc.streamFailure {
				require.NotNil(t, taskErr)
				assert.True(t, taskErr.NoRetry)
				return
			}
			require.Nil(t, taskErr)
			assert.Equal(t, model.TaskStatusSuccess, result.Immediate.Status)
			var data struct {
				Data []map[string]any `json:"data"`
			}
			require.NoError(t, common.Unmarshal(result.TaskData, &data))
			require.Len(t, data.Data, count)
			for _, item := range data.Data {
				assert.Equal(t, "YQ==", item["b64_json"])
			}
			facts, err := adaptor.ExtractUsageFactsValidated(c, info)
			require.NoError(t, err)
			assert.EqualValues(t, count, facts["images"])
			assert.False(t, c.Writer.Written())
		})
	}
}

func TestBackgroundImageFramingDiagnostics(t *testing.T) {
	source, err := plugins.Source("openai-image")
	require.NoError(t, err)
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	for _, tc := range []struct{ name, body, summary string }{
		{"JSON伪装为SSE", "{\n  \"data\": [{\"b64_json\":\"PRIVATE_IMAGE\"}]\n}\n", "data_lines=0 event_lines=0 blank_lines=0 comment_lines=0 unknown_lines=3 indented_lines=1 pending_data_lines=0 first_content=json_like"},
		{"缺少末尾空行", "event: image_edit.completed\ndata: {\"b64_json\":\"PRIVATE_IMAGE\"}\n", "data_lines=1 event_lines=1 blank_lines=0 comment_lines=0 unknown_lines=0 indented_lines=0 pending_data_lines=1 first_content=sse_field"},
		{"缩进字段", " data: PRIVATE_IMAGE\n\n", "data_lines=0 event_lines=0 blank_lines=1 comment_lines=0 unknown_lines=1 indented_lines=1 pending_data_lines=0 first_content=sse_field"},
		{"只有心跳", ": PRIVATE_IMAGE\n\n", "data_lines=0 event_lines=0 blank_lines=1 comment_lines=1 unknown_lines=0 indented_lines=0 pending_data_lines=0 first_content=none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adaptor := New(plugin)
			adaptor.diagnostics = &types.TaskDiagnostics{}
			response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(tc.body))}
			_, err := adaptor.readSubmitEvents(t.Context(), response, map[string]any{"publicTaskId": "task_framing", "requestBody": map[string]any{"action": "edit", "n": 1}})
			if tc.name == "JSON伪装为SSE" {
				require.NoError(t, err)
				assert.Equal(t, "body", adaptor.diagnostics.ResponseKind)
				assert.NotContains(t, adaptor.diagnostics.ResponseBody, "PRIVATE_IMAGE")
				return
			}
			require.ErrorContains(t, err, "ended before")
			assert.Equal(t, "sse_framing_summary", adaptor.diagnostics.ResponseKind)
			assert.Equal(t, tc.summary, adaptor.diagnostics.ResponseBody)
			assert.NotContains(t, adaptor.diagnostics.ResponseBody, "PRIVATE_IMAGE")
		})
	}
}

func TestBackgroundImageJSONFallback(t *testing.T) {
	source, err := plugins.Source("openai-image")
	require.NoError(t, err)
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, media, body string
		ok                bool
	}{
		{"心跳后JSON", "text/event-stream", ": keep-alive\n\n{\n  \"data\": [{\"b64_json\":\"YQ==\"}]\n}\n", true},
		{"普通JSON", "application/json", `{"data":[{"url":"https://example.com/result.png"}]}`, true},
		{"错误对象", "text/event-stream", `{"error":{"message":"failed"}}`, false},
		{"空图片", "text/event-stream", `{"data":[{}]}`, false},
		{"图片数量不符", "text/event-stream", `{"data":[{"b64_json":"YQ=="},{"b64_json":"Yg=="}]}`, false},
		{"截断JSON", "text/event-stream", `{"data":[{"b64_json":"YQ=="}`, false},
		{"JSON后混入SSE", "text/event-stream", "{\"data\":[{\"b64_json\":\"YQ==\"}]}\ndata: [DONE]\n\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := New(plugin)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://example.com"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_json"}, OriginModelName: "gpt-image-1.5"}
			a.Init(info)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/tasks/openai-image", nil)
			c.Set("task_request", map[string]any{"prompt": "编辑", "images": []any{map[string]any{"file_id": "file-input"}}})
			require.Nil(t, a.ValidateRequestAndSetAction(c, info))
			result, taskErr := a.ParseResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{tc.media}}, Body: io.NopCloser(strings.NewReader(tc.body))}, info)
			if !tc.ok {
				require.NotNil(t, taskErr)
				assert.True(t, taskErr.NoRetry)
				return
			}
			require.Nil(t, taskErr)
			assert.Equal(t, model.TaskStatusSuccess, result.Immediate.Status)
			assert.Contains(t, string(result.TaskData), `"mime_type":"image/png"`)
		})
	}
}
