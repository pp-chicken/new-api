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
