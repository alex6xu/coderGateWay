package provider

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/alex/codegateway/internal/claudeoauth"
	"github.com/google/uuid"
)

// Claude Code wire versions — pinned to OmniRoute CLAUDE_CODE_CLIENT_*.
const (
	claudeCodeVersion              = claudeoauth.ClaudeCodeVersion
	claudeCodeBillingVersion       = claudeCodeVersion + ".250"
	claudeCodeStainlessPkgVersion  = "0.94.0"
	claudeCodeStainlessRuntimeVer  = "v26.3.0"
	claudeCodeUserAgent            = claudeoauth.ClaudeCodeUserAgent
	claudeCodeSentinel             = "You are Claude Code, Anthropic's official CLI for Claude."
	claudeCodeBillingPrefix        = "x-anthropic-billing-header:"
)

var oauthSessionCache sync.Map // seed -> session UUID

func oauthSessionID(seed string) string {
	if seed == "" {
		seed = "anon"
	}
	if v, ok := oauthSessionCache.Load(seed); ok {
		return v.(string)
	}
	id := uuid.NewString()
	actual, _ := oauthSessionCache.LoadOrStore(seed, id)
	return actual.(string)
}

func stainlessOS() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		return "MacOS"
	case "linux":
		return "Linux"
	case "freebsd":
		return "FreeBSD"
	default:
		return "Unknown"
	}
}

func stainlessArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	case "386":
		return "x32"
	default:
		return runtime.GOARCH
	}
}

func buildClaudeUserIDJSON(deviceID, accountUUID, sessionID string) string {
	b, _ := json.Marshal(map[string]string{
		"device_id":    deviceID,
		"account_uuid": accountUUID,
		"session_id":   sessionID,
	})
	return string(b)
}

func ensureDeviceID(id string) string {
	if len(id) == 64 && isHex(id) {
		return id
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func modelHasPrefix(model string, prefixes []string) bool {
	m := strings.ToLower(model)
	for _, p := range prefixes {
		if strings.Contains(m, p) {
			return true
		}
	}
	return false
}

// selectClaudeOAuthBetas mirrors OmniRoute selectBetaFlags for opaque (cloaked) clients.
func selectClaudeOAuthBetas(model string, hasSystem, hasTools, hasStructuredOutput bool) string {
	isFullAgent := hasTools && hasSystem
	isHeavy := isFullAgent && modelHasPrefix(model, []string{"claude-opus", "claude-sonnet"})
	isOpusAgent := isFullAgent && modelHasPrefix(model, []string{"claude-opus"})
	isContext1m := isOpusAgent && !modelHasPrefix(model, []string{"claude-opus-5"})

	flags := make([]string, 0, 16)
	if isFullAgent {
		flags = append(flags, "claude-code-20250219")
	}
	flags = append(flags, "oauth-2025-04-20")
	if isContext1m {
		flags = append(flags, "context-1m-2025-08-07")
	}
	if isOpusAgent {
		flags = append(flags, "mid-conversation-system-2026-04-07")
	}
	flags = append(flags,
		"interleaved-thinking-2025-05-14",
		"redact-thinking-2026-02-12",
		"thinking-token-count-2026-05-13",
		"context-management-2025-06-27",
		"prompt-caching-scope-2026-01-05",
	)
	if hasStructuredOutput || isFullAgent {
		flags = append(flags, "advisor-tool-2026-03-01")
	}
	if hasStructuredOutput && !isFullAgent {
		flags = append(flags, "structured-outputs-2025-12-15")
	}
	if isFullAgent {
		flags = append(flags, "extended-cache-ttl-2025-04-11", "cache-diagnosis-2026-04-07")
	}
	if isHeavy {
		flags = append(flags, "advanced-tool-use-2025-11-20", "effort-2025-11-24")
	}
	// OmniRoute CLAUDE_OAUTH_EXTRA_BETAS — always useful for streaming tools.
	flags = append(flags, "fine-grained-tool-streaming-2025-05-14")
	return strings.Join(flags, ",")
}

func claudeBillingLine() string {
	return fmt.Sprintf(
		"%s cc_version=%s; cc_entrypoint=cli; cch=00000;",
		claudeCodeBillingPrefix,
		claudeCodeBillingVersion,
	)
}
