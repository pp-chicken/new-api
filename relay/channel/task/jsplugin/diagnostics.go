package jsplugin

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

var diagnosticSecrets = regexp.MustCompile(`(?i)(bearer\s+[^\s"<>]+|sk-[a-z0-9_-]+|data:[^\s"<>]*;base64,[a-z0-9+/=]+)`)
var diagnosticAssignments = regexp.MustCompile(`(?i)("?(?:api[_-]?key|key|authorization|password|secret|token|access[_-]?token|refresh[_-]?token|cookie|b64_json|partial_image_b64|base64)"?\s*[:=]\s*)("(?:\\.|[^"\\])*"?|[^\s,}\r\n]+)`)

func (a *TaskAdaptor) TaskDiagnostics() *types.TaskDiagnostics { return a.diagnostics }

// diagnosticBody 先脱敏再截断，避免截断 JSON 后暴露密钥；文本错误也走相同保护。
func (a *TaskAdaptor) diagnosticBody(body string) string {
	var value any
	if common.UnmarshalJsonStr(body, &value) == nil {
		value = a.diagnosticValue(value, 0)
		if encoded, err := common.Marshal(value); err == nil {
			if indented, err := common.IndentJson(encoded); err == nil {
				encoded = indented
			}
			body = string(encoded)
		}
	}
	if a.info != nil && a.info.ApiKey != "" {
		body = strings.ReplaceAll(body, a.info.ApiKey, "[REDACTED]")
	}
	if a.submit != nil {
		for name, value := range a.submit.Headers {
			key := strings.ToLower(name)
			if value != "" && (strings.Contains(key, "auth") || strings.Contains(key, "key") || strings.Contains(key, "token") || strings.Contains(key, "cookie")) {
				body = strings.ReplaceAll(body, value, "[REDACTED]")
				if _, token, ok := strings.Cut(value, " "); ok && token != "" {
					body = strings.ReplaceAll(body, token, "[REDACTED]")
				}
			}
		}
	}
	body = diagnosticSecrets.ReplaceAllString(body, "[REDACTED]")
	body = diagnosticAssignments.ReplaceAllString(body, `${1}"[REDACTED]"`)
	const limit = 16 << 10
	if len(body) > limit {
		body = strings.ToValidUTF8(body[:limit], "") + "\n[TRUNCATED]"
	}
	return body
}

func (a *TaskAdaptor) diagnosticValue(value any, depth int) any {
	if depth > 32 {
		return "[OMITTED]"
	}
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
			switch normalized {
			case "apikey", "key", "authorization", "password", "secret", "token", "accesstoken", "refreshtoken", "cookie", "b64json", "partialimageb64", "base64":
				v[key] = "[REDACTED]"
			default:
				v[key] = a.diagnosticValue(child, depth+1)
			}
		}
	case []any:
		for i := range v {
			v[i] = a.diagnosticValue(v[i], depth+1)
		}
	case string:
		if strings.HasPrefix(v, "data:") {
			return "[OMITTED]"
		}
		if parsed, err := url.Parse(v); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
			parsed.User, parsed.RawQuery, parsed.Fragment = nil, "", ""
			return parsed.String()
		}
	}
	return value
}

func (a *TaskAdaptor) TaskFailure() *types.TaskFailure { return a.failure }

// 只提取 JSON 错误字段，HTML、原始文本及运行时异常不作为公开错误返回。
func (a *TaskAdaptor) captureTaskFailure(body string) {
	var payload map[string]any
	if common.UnmarshalJsonStr(body, &payload) != nil {
		return
	}
	failure, ok := payload["error"].(map[string]any)
	if !ok {
		eventType, _ := payload["type"].(string)
		if eventType != "error" && eventType != "upstream_error" && (a.diagnostics == nil || a.diagnostics.StatusCode < 400) {
			return
		}
		failure = payload
	}
	message, _ := failure["message"].(string)
	if strings.TrimSpace(message) == "" {
		return
	}
	// 内部地址可能出现在自由文本中，先隐藏 URL 再执行凭据脱敏。
	message = publicErrorURL.ReplaceAllString(message, "[URL REDACTED]")
	message = a.diagnosticBody(message)
	// 多行运行时堆栈只保留第一行错误摘要。
	message, _, _ = strings.Cut(message, "\n")
	message = strings.TrimSpace(message)
	if strings.ContainsAny(message, "<>") {
		return
	}
	if len(message) > 1024 {
		message = strings.ToValidUTF8(message[:1024], "") + "…"
	}
	code, _ := failure["code"].(string)
	if !publicErrorCode.MatchString(code) {
		code = "upstream_error"
	}
	code = a.diagnosticBody(code)
	a.failure = &types.TaskFailure{Code: code, Message: message}
	if a.diagnostics != nil {
		a.failure.UpstreamStatus = a.diagnostics.StatusCode
	}
}

var publicErrorURL = regexp.MustCompile(`(?i)https?://[^\s"<>]+`)
var publicErrorCode = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,128}$`)
