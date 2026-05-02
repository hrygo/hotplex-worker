package messaging

import (
	"encoding/json"
	"strings"

	"github.com/hrygo/hotplex/pkg/events"
)

// SecurityErrorCode identifies a category of security or session error.
type SecurityErrorCode int

const (
	SecErrForbiddenDir SecurityErrorCode = iota
	SecErrUnderDir
	SecErrNotInWhitelist
	SecErrMustBeAbsolute
	SecErrMustNotBeEmpty
	SecErrPolicyRejected
	SecErrSessionNotActive
	SecErrSessionNotFound
	SecErrExpandWorkDir
	SecErrWorkerTerminate
	SecErrStartSession
)

// SecurityMessagesCN maps security error codes to Chinese user-facing messages.
var SecurityMessagesCN = map[SecurityErrorCode]string{
	SecErrForbiddenDir:     "🚫 禁止访问系统目录",
	SecErrUnderDir:         "🚫 目录被安全策略禁止（系统关键目录）",
	SecErrNotInWhitelist:   "🚫 目录未在允许列表中（需在 config.yaml 中配置 security.work_dir_allowed_base_patterns）",
	SecErrMustBeAbsolute:   "🚫 路径必须是绝对路径（以 / 开头）",
	SecErrMustNotBeEmpty:   "🚫 工作目录不能为空",
	SecErrPolicyRejected:   "🚫 安全策略拒绝",
	SecErrSessionNotActive: "⚠️ 会话未激活（请先发送消息启动会话）",
	SecErrSessionNotFound:  "⚠️ 会话不存在",
	SecErrExpandWorkDir:    "📁 路径展开失败（请检查路径格式）",
	SecErrWorkerTerminate:  "⚠️ 停止原工作进程失败",
	SecErrStartSession:     "⚠️ 启动新会话失败",
}

// SecurityMessagesEN maps security error codes to English user-facing messages.
var SecurityMessagesEN = map[SecurityErrorCode]string{
	SecErrForbiddenDir:     ":no_entry_sign: Forbidden system directory",
	SecErrUnderDir:         ":no_entry_sign: Directory blocked by security policy (system directory)",
	SecErrNotInWhitelist:   ":no_entry_sign: Directory not allowed (configure `security.work_dir_allowed_base_patterns` in config.yaml)",
	SecErrMustBeAbsolute:   ":no_entry_sign: Path must be absolute (start with /)",
	SecErrMustNotBeEmpty:   ":no_entry_sign: Work directory cannot be empty",
	SecErrPolicyRejected:   ":no_entry_sign: Security policy rejected",
	SecErrSessionNotActive: ":warning: Session not active (send a message first to start)",
	SecErrSessionNotFound:  ":warning: Session not found",
	SecErrExpandWorkDir:    ":file_folder: Path expansion failed (check path format)",
	SecErrWorkerTerminate:  ":warning: Failed to stop previous worker",
	SecErrStartSession:     ":warning: Failed to start new session",
}

// FormatSecurityError converts a technical error into a user-friendly message
// using the provided locale message map. Returns the original error message
// as fallback when no known pattern matches.
func FormatSecurityError(err error, msgs map[SecurityErrorCode]string) string {
	if err == nil {
		return ""
	}
	errMsg := err.Error()

	// Security-related errors (nested pattern matching)
	if strings.Contains(errMsg, "security: work dir") {
		securityChecks := []struct {
			substr string
			code   SecurityErrorCode
		}{
			{"forbidden system directory", SecErrForbiddenDir},
			{"under forbidden directory", SecErrUnderDir},
			{"not in whitelist", SecErrNotInWhitelist},
			{"must be absolute", SecErrMustBeAbsolute},
			{"must not be empty", SecErrMustNotBeEmpty},
		}
		for _, check := range securityChecks {
			if strings.Contains(errMsg, check.substr) {
				return msgs[check.code]
			}
		}
		return msgs[SecErrPolicyRejected]
	}

	// Session and work directory errors
	errorChecks := []struct {
		substr string
		code   SecurityErrorCode
	}{
		{"session not active", SecErrSessionNotActive},
		{"get session", SecErrSessionNotFound},
		{"expand work dir", SecErrExpandWorkDir},
		{"worker terminate failed", SecErrWorkerTerminate},
		{"start session", SecErrStartSession},
	}
	for _, check := range errorChecks {
		if strings.Contains(errMsg, check.substr) {
			return msgs[check.code]
		}
	}

	// Clean switch-workdir technical prefix for clearer output.
	if strings.Contains(errMsg, "switch-workdir") {
		cleanMsg := strings.ReplaceAll(errMsg, "switch-workdir-inplace: ", "")
		cleanMsg = strings.ReplaceAll(cleanMsg, "switch-workdir: ", "")
		return cleanMsg
	}

	return errMsg
}

// ExtractMCPStatusData extracts MCP status data from an event envelope.
func ExtractMCPStatusData(env *events.Envelope) (events.MCPStatusData, bool) {
	var d events.MCPStatusData
	switch v := env.Event.Data.(type) {
	case events.MCPStatusData:
		d = v
	case map[string]any:
		raw, _ := json.Marshal(v)
		_ = json.Unmarshal(raw, &d)
	default:
		return d, false
	}
	return d, true
}

// MCPServerIcon returns the status icon for an MCP server.
func MCPServerIcon(status string) string {
	if status == "connected" || status == "ok" {
		return "✅"
	}
	return "❌"
}
