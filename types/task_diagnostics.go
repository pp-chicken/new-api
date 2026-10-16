package types

// TaskDiagnostics 仅通过任务日志的超级管理员视图返回，不进入公开任务结果。
type TaskDiagnostics struct {
	Method            string `json:"method"`
	URL               string `json:"url"`
	StatusCode        int    `json:"status_code"`
	RequestBody       string `json:"request_body"`
	ResponseBody      string `json:"response_body"`
	ResponseKind      string `json:"response_kind"`
	ErrorCode         string `json:"error_code,omitempty"`
	ErrorMessage      string `json:"error_message,omitempty"`
	UpstreamRequestID string `json:"upstream_request_id,omitempty"`
}

// TaskFailure 是可公开的错误摘要，不包含诊断正文或请求信息。
type TaskFailure struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	UpstreamStatus int    `json:"upstream_status,omitempty"`
}
