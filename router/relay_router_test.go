package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/networktest"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var networkTestUserSequence atomic.Int64

func TestNetworkTestRouteAuthenticationAndLimits(t *testing.T) {
	setupRelayRouterTestDB(t)
	require.NoError(t, i18n.Init())
	engine := gin.New()
	engine.Use(middleware.RequestId())
	SetRelayRouter(engine)
	server := httptest.NewServer(engine)
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/network-test?bytes=65536"
	dialer := websocket.Dialer{Subprotocols: []string{networktest.Subprotocol}, HandshakeTimeout: time.Second}
	newToken := func(userStatus, tokenStatus int, expires int64, ip *string) string {
		t.Helper()
		id := int(networkTestUserSequence.Add(1)) + 10000
		user := model.User{Id: id, Username: fmt.Sprintf("network-user-%d", id), AffCode: fmt.Sprint(id), Status: userStatus, Group: "default"}
		require.NoError(t, model.DB.Create(&user).Error)
		key := fmt.Sprintf("networktest%d", id)
		require.NoError(t, model.DB.Create(&model.Token{
			UserId: id, Key: key, Status: tokenStatus, ExpiredTime: expires,
			UnlimitedQuota: true, AllowIps: ip, ModelLimitsEnabled: true, ModelLimits: "no-test-model",
		}).Error)
		return key
	}
	blockedIP := "192.0.2.1"
	for _, tc := range []struct {
		name   string
		key    string
		query  string
		origin string
		status int
	}{
		{name: "缺少令牌", status: http.StatusUnauthorized},
		{name: "不接受查询参数中的令牌", query: "&key=" + newToken(common.UserStatusEnabled, common.TokenStatusEnabled, -1, nil), status: http.StatusUnauthorized},
		{name: "过期令牌", key: newToken(common.UserStatusEnabled, common.TokenStatusEnabled, time.Now().Add(-time.Hour).Unix(), nil), status: http.StatusUnauthorized},
		{name: "禁用令牌", key: newToken(common.UserStatusEnabled, common.TokenStatusDisabled, -1, nil), status: http.StatusUnauthorized},
		{name: "禁用用户", key: newToken(common.UserStatusDisabled, common.TokenStatusEnabled, -1, nil), status: http.StatusForbidden},
		{name: "IP 不允许", key: newToken(common.UserStatusEnabled, common.TokenStatusEnabled, -1, &blockedIP), status: http.StatusForbidden},
		{name: "跨站 Origin", key: newToken(common.UserStatusEnabled, common.TokenStatusEnabled, -1, nil), origin: "https://untrusted.example", status: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header := make(http.Header)
			if tc.key != "" {
				header.Set("Authorization", "Bearer "+tc.key)
			}
			if tc.origin != "" {
				header.Set("Origin", tc.origin)
			}
			conn, response, err := dialer.Dial(endpoint+tc.query, header)
			if conn != nil {
				_ = conn.Close()
			}
			require.Error(t, err)
			require.NotNil(t, response)
			defer response.Body.Close()
			assert.Equal(t, tc.status, response.StatusCode)
		})
	}

	t.Run("真实路由完成传输且不需要模型通道", func(t *testing.T) {
		key := newToken(common.UserStatusEnabled, common.TokenStatusEnabled, -1, nil)
		conn, response, err := dialer.Dial(endpoint, http.Header{"Authorization": {"Bearer " + key}})
		require.NoError(t, err)
		defer conn.Close()
		assert.NotEmpty(t, response.Header.Get(common.RequestIdKey))
		assert.Empty(t, response.Header.Get("Sec-WebSocket-Extensions"))
		result, err := networktest.Run(conn, networktest.MinBytes, time.Second)
		require.NoError(t, err)
		assert.Equal(t, networktest.MinBytes, result.Bytes)
		assert.Positive(t, result.Upload)
		assert.Positive(t, result.Download)
	})

	t.Run("握手前拒绝超大测试", func(t *testing.T) {
		key := newToken(common.UserStatusEnabled, common.TokenStatusEnabled, -1, nil)
		conn, response, err := dialer.Dial(strings.Replace(endpoint, "65536", "33554433", 1), http.Header{"Authorization": {"Bearer " + key}})
		if conn != nil {
			_ = conn.Close()
		}
		require.Error(t, err)
		require.NotNil(t, response)
		defer response.Body.Close()
		assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	})

	t.Run("同用户并发连接受限", func(t *testing.T) {
		key := newToken(common.UserStatusEnabled, common.TokenStatusEnabled, -1, nil)
		header := http.Header{"Authorization": {"Bearer " + key}}
		first, _, err := dialer.Dial(endpoint, header)
		require.NoError(t, err)
		defer first.Close()
		second, response, err := dialer.Dial(endpoint, header)
		if second != nil {
			_ = second.Close()
		}
		require.Error(t, err)
		require.NotNil(t, response)
		defer response.Body.Close()
		assert.Equal(t, http.StatusTooManyRequests, response.StatusCode)
		assert.NotEmpty(t, response.Header.Get("Retry-After"))
	})

	t.Run("调用频率受限", func(t *testing.T) {
		key := newToken(common.UserStatusEnabled, common.TokenStatusEnabled, -1, nil)
		for i := range 4 {
			request, err := http.NewRequest(http.MethodGet, server.URL+"/v1/network-test?bytes=1", nil)
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer "+key)
			response, err := http.DefaultClient.Do(request)
			require.NoError(t, err)
			_ = response.Body.Close()
			if i < 3 {
				assert.Equal(t, http.StatusBadRequest, response.StatusCode)
			} else {
				assert.Equal(t, http.StatusTooManyRequests, response.StatusCode)
				assert.NotEmpty(t, response.Header.Get("Retry-After"))
			}
		}
	})

	t.Run("稳定性测试在连接期间重新验证访问权限", func(t *testing.T) {
		stabilityDialer := websocket.Dialer{Subprotocols: []string{networktest.StabilitySubprotocol}, HandshakeTimeout: time.Second}
		var connections []*websocket.Conn
		for _, change := range []string{"令牌禁用", "令牌过期", "用户禁用", "IP 变更"} {
			key := newToken(common.UserStatusEnabled, common.TokenStatusEnabled, -1, nil)
			conn, response, err := stabilityDialer.Dial(strings.Replace(endpoint, "bytes=65536", "duration_ms=60000", 1), http.Header{"Authorization": {"Bearer " + key}})
			require.NoError(t, err)
			defer conn.Close()
			assert.NotEmpty(t, response.Header.Get(common.RequestIdKey))
			require.NoError(t, networktest.AwaitStabilityReady(conn, time.Second))
			_, err = networktest.ProbeStability(conn, 1, time.Second)
			require.NoError(t, err)
			connections = append(connections, conn)
			switch change {
			case "令牌禁用":
				require.NoError(t, model.DB.Model(&model.Token{}).Where("key = ?", key).Update("status", common.TokenStatusDisabled).Error)
			case "令牌过期":
				require.NoError(t, model.DB.Model(&model.Token{}).Where("key = ?", key).Update("expired_time", time.Now().Add(-time.Minute).Unix()).Error)
			case "用户禁用":
				var token model.Token
				require.NoError(t, model.DB.Where("key = ?", key).First(&token).Error)
				require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", token.UserId).Update("status", common.UserStatusDisabled).Error)
			case "IP 变更":
				require.NoError(t, model.DB.Model(&model.Token{}).Where("key = ?", key).Update("allow_ips", "192.0.2.1").Error)
			}
		}
		// 等待正常探测间隔，确保拒绝原因来自权限变更，而不是频率限制。
		<-time.After(networktest.StabilityInterval)
		for _, conn := range connections {
			_, err := networktest.ProbeStability(conn, 2, time.Second)
			var closeErr *websocket.CloseError
			require.ErrorAs(t, err, &closeErr)
			assert.Equal(t, websocket.ClosePolicyViolation, closeErr.Code)
			assert.Equal(t, "测试令牌已失效或访问被拒绝", closeErr.Text)
		}
	})

	t.Run("稳定性测试时长边界在握手前校验", func(t *testing.T) {
		key := newToken(common.UserStatusEnabled, common.TokenStatusEnabled, -1, nil)
		stabilityDialer := websocket.Dialer{Subprotocols: []string{networktest.StabilitySubprotocol}, HandshakeTimeout: time.Second}
		for _, milliseconds := range []string{"999", "86400001", "9223372036854775807"} {
			conn, response, err := stabilityDialer.Dial(strings.Replace(endpoint, "bytes=65536", "duration_ms="+milliseconds, 1), http.Header{"Authorization": {"Bearer " + key}})
			if conn != nil {
				_ = conn.Close()
			}
			require.Error(t, err)
			require.NotNil(t, response)
			_ = response.Body.Close()
			assert.Equal(t, http.StatusBadRequest, response.StatusCode)
		}
	})
}

func TestListModelsSupportsOpenAIAndGeminiAuthentication(t *testing.T) {
	setupRelayRouterTestDB(t)

	user := model.User{
		Username: "models-user",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100,
	}
	require.NoError(t, model.DB.Create(&user).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		UserId:         user.Id,
		Key:            "modelstestkey",
		Status:         common.TokenStatusEnabled,
		ExpiredTime:    -1,
		UnlimitedQuota: true,
	}).Error)

	engine := gin.New()
	SetRelayRouter(engine)

	tests := []struct {
		name           string
		path           string
		headerName     string
		expectedObject string
		expectedField  string
	}{
		{
			name:           "OpenAI bearer token",
			path:           "/v1/models",
			headerName:     "Authorization",
			expectedObject: "list",
			expectedField:  "data",
		},
		{
			name:          "Gemini API key header",
			path:          "/v1/models",
			headerName:    "x-goog-api-key",
			expectedField: "models",
		},
		{
			name:          "Gemini API key query",
			path:          "/v1/models?key=modelstestkey",
			expectedField: "models",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			if test.headerName != "" {
				value := "modelstestkey"
				if test.headerName == "Authorization" {
					value = "Bearer " + value
				}
				request.Header.Set(test.headerName, value)
			}

			engine.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code)
			var payload map[string]any
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
			assert.Contains(t, payload, test.expectedField)
			assert.NotContains(t, payload, "error")
			if test.expectedObject != "" {
				assert.Equal(t, test.expectedObject, payload["object"])
			}
		})
	}
}

func setupRelayRouterTestDB(t *testing.T) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	originalIsMasterNode := common.IsMasterNode
	originalRedisEnabled := common.RedisEnabled
	originalSQLitePath := common.SQLitePath
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")

	common.IsMasterNode = false
	common.RedisEnabled = false
	common.SQLitePath = fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, os.Setenv("SQL_DSN", "local"))
	require.NoError(t, model.InitDB())
	model.LOG_DB = model.DB
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.Token{}, &model.Ability{}))

	t.Cleanup(func() {
		if sqlDB, err := model.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		common.IsMasterNode = originalIsMasterNode
		common.RedisEnabled = originalRedisEnabled
		common.SQLitePath = originalSQLitePath
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	})
}

// 验证真实路由、中间件和目录协议，避免只测试 JSON 拼装。
func TestCodexModelCatalog(t *testing.T) {
	setupRelayRouterTestDB(t)
	require.NoError(t, i18n.Init())
	originalSelfUseMode := operation_setting.SelfUseModeEnabled
	operation_setting.SelfUseModeEnabled = false
	t.Cleanup(func() { operation_setting.SelfUseModeEnabled = originalSelfUseMode })
	originalRatios := ratio_setting.ModelRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(ratio_setting.DefaultModelRatio2JSONString()))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalRatios)) })
	require.True(t, helper.HasModelBillingConfig("gpt-5.6-sol"))
	require.True(t, helper.HasModelBillingConfig("gpt-5.6-terra"))
	require.False(t, helper.HasModelBillingConfig("gpt-daybreak-blue-latest"))

	user := model.User{Username: "codex-catalog-user", Status: common.UserStatusEnabled, Group: "default", Quota: 100}
	require.NoError(t, model.DB.Create(&user).Error)
	require.NoError(t, model.DB.Create(&[]model.Ability{
		{Group: "default", Model: "gpt-5.6-sol", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "gpt-5.6-sol", ChannelId: 2, Enabled: true},
		{Group: "default", Model: "gpt-5.6-terra", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "gpt-5.6-luna", ChannelId: 1, Enabled: false},
		{Group: "private", Model: "gpt-5.5", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "gpt-image-1", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "unknown-codex-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "gpt-daybreak-blue-latest", ChannelId: 1, Enabled: true},
	}).Error)
	for _, token := range []model.Token{
		{Key: "catalogall"},
		{Key: "cataloglimited", ModelLimitsEnabled: true, ModelLimits: "gpt-5.6-sol,gpt-5.5"},
		{Key: "catalogempty", ModelLimitsEnabled: true},
	} {
		token.UserId = user.Id
		token.Status = common.TokenStatusEnabled
		token.ExpiredTime = -1
		token.UnlimitedQuota = true
		require.NoError(t, model.DB.Create(&token).Error)
	}
	require.NoError(t, model.DB.Create(&model.Token{UserId: user.Id, Key: "catalogexpired", Status: common.TokenStatusEnabled, ExpiredTime: 1, UnlimitedQuota: true}).Error)
	require.NoError(t, model.DB.Create(&model.Token{UserId: user.Id, Key: "catalogdisabled", Status: common.TokenStatusDisabled, ExpiredTime: -1, UnlimitedQuota: true}).Error)

	engine := gin.New()
	SetRelayRouter(engine)
	for _, tc := range []struct {
		name    string
		key     string
		status  int
		models  []string
		selfUse bool
	}{
		{name: "可见目录", key: "catalogall", status: http.StatusOK, models: []string{"gpt-5.6-sol", "gpt-5.6-terra"}},
		{name: "令牌限制与分组隔离", key: "cataloglimited", status: http.StatusOK, models: []string{"gpt-5.6-sol"}},
		{name: "空白名单返回空数组", key: "catalogempty", status: http.StatusOK, models: []string{}},
		{name: "自用模式允许未定价模型", key: "catalogall", status: http.StatusOK, selfUse: true, models: []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-daybreak-blue-latest"}},
		{name: "缺少令牌", status: http.StatusUnauthorized},
		{name: "无效令牌", key: "invalid", status: http.StatusUnauthorized},
		{name: "过期令牌", key: "catalogexpired", status: http.StatusUnauthorized},
		{name: "停用令牌", key: "catalogdisabled", status: http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.SelfUseModeEnabled = tc.selfUse
			request := httptest.NewRequest(http.MethodGet, "/v1/codex/models?client_version=0.166.0", nil)
			if tc.key != "" {
				request.Header.Set("Authorization", "Bearer sk-"+tc.key)
			}
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			assert.Contains(t, recorder.Header().Get("Cache-Control"), "no-store")
			var payload map[string]any
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
			if tc.status != http.StatusOK {
				assert.Contains(t, payload, "error")
				assert.NotContains(t, payload, "models")
				return
			}
			assert.Len(t, payload, 1)
			models, ok := payload["models"].([]any)
			require.True(t, ok, "models 必须是数组，不能为 null")
			slugs := make([]string, 0, len(models))
			for _, item := range models {
				entry := item.(map[string]any)
				slug := entry["slug"].(string)
				slugs = append(slugs, slug)
				for _, key := range []string{"display_name", "description", "supported_reasoning_levels", "shell_type", "visibility", "supported_in_api", "priority", "support_verbosity", "truncation_policy", "experimental_supported_tools", "input_modalities", "context_window"} {
					assert.Contains(t, entry, key)
				}
				assert.Equal(t, true, entry["supported_in_api"])
				assert.Equal(t, false, entry["prefer_websockets"])
				assert.Equal(t, false, entry["use_responses_lite"])
				assert.Equal(t, false, entry["supports_reasoning_effort_updates"])
				assert.NotEmpty(t, entry["base_instructions"])
				assert.Equal(t, entry["base_instructions"], entry["model_messages"].(map[string]any)["instructions_template"])
				levels := entry["supported_reasoning_levels"].([]any)
				efforts := make([]string, 0, len(levels))
				for _, level := range levels {
					efforts = append(efforts, level.(map[string]any)["effort"].(string))
				}
				assert.Contains(t, efforts, entry["default_reasoning_level"])
				if slug == "gpt-5.6-sol" {
					assert.Equal(t, []string{"low", "medium", "high", "xhigh", "max", "ultra"}, efforts)
					assert.Equal(t, float64(272000), entry["context_window"])
				}
			}
			assert.Equal(t, tc.models, slugs)
		})
	}
}
