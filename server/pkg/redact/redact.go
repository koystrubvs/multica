// Package redact provides functions for detecting and masking secrets
// in agent output before it reaches the database or WebSocket broadcast.
package redact

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/user"
	"regexp"
	"strings"
)

// secretPattern pairs a compiled regex with its replacement text.
type secretPattern struct {
	re          *regexp.Regexp
	replacement string
}

// Patterns are checked in order; first match wins per position.
var patterns = []secretPattern{
	// AWS access key IDs (always start with AKIA)
	{regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), "[REDACTED AWS KEY]"},

	// AWS secret access keys (40 char base64-ish, preceded by a common separator)
	{regexp.MustCompile(`(?i)(?:aws_secret_access_key|secret_?access_?key)\s*[=:]\s*[A-Za-z0-9/+=]{40}`), "[REDACTED AWS SECRET]"},

	// PEM private keys (multi-line)
	{regexp.MustCompile(`(?s)-----BEGIN[A-Z\s]*PRIVATE KEY-----.*?-----END[A-Z\s]*PRIVATE KEY-----`), "[REDACTED PRIVATE KEY]"},

	// GitHub tokens (classic PAT, OAuth, user-to-server, server-to-server, refresh)
	{regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,255}\b`), "[REDACTED GITHUB TOKEN]"},

	// GitHub fine-grained personal access tokens use the github_pat_ prefix,
	// which the classic ghp_/gho_/... pattern above does not cover. Without
	// this line a fine-grained PAT emitted in agent output leaks unredacted
	// to the database and WebSocket broadcast.
	{regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,255}\b`), "[REDACTED GITHUB TOKEN]"},

	// OpenAI / Anthropic API keys
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`), "[REDACTED API KEY]"},

	// Slack bot/user/legacy tokens. The char class includes 'e' so the
	// newer xoxe- config/refresh tokens are covered alongside xoxb/p/o/r/a/s.
	{regexp.MustCompile(`\bxox[bporase]-[A-Za-z0-9\-]{10,}\b`), "[REDACTED SLACK TOKEN]"},

	// Slack app-level tokens use the xapp- prefix, which the xox*- rule above
	// does not match. Without this an app-level token echoed in agent output
	// leaks unredacted to the DB / WebSocket broadcast.
	{regexp.MustCompile(`\bxapp-[A-Za-z0-9-]{10,}\b`), "[REDACTED SLACK TOKEN]"},

	// GitLab personal access tokens
	{regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`), "[REDACTED GITLAB TOKEN]"},

	// Google API keys always start with the AIza prefix and are 39 chars total
	// (AIza + 35). Capture and restore the trailing delimiter so keys ending in
	// a non-word character such as '-' are still redacted.
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}([^0-9A-Za-z_-]|$)`), "[REDACTED GOOGLE API KEY]$1"},

	// Stripe secret / restricted live keys (sk_live_ / rk_live_). The sk-
	// rule above only matches the hyphen form used by OpenAI/Anthropic; Stripe
	// uses an underscore, so live keys are not covered without this. Publishable
	// keys (pk_live_) are intentionally excluded — they are not secret.
	{regexp.MustCompile(`\b(?:sk|rk)_live_[0-9A-Za-z]{16,}\b`), "[REDACTED STRIPE KEY]"},

	// JWT tokens (three base64url segments)
	{regexp.MustCompile(`\bey[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`), "[REDACTED JWT]"},

	// Generic "Bearer <token>" in output
	{regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-._~+/]+=*\b`), "Bearer [REDACTED]"},

	// Connection strings with embedded passwords
	{regexp.MustCompile(`(?i)(?:postgres|mysql|mongodb|redis|amqp)(?:ql)?://[^:\s]+:[^@\s]+@`), "[REDACTED CONNECTION STRING]@"},

	// Generic key=value patterns for common secret env var names
	{regexp.MustCompile(`(?i)(?:API_KEY|API_SECRET|SECRET_KEY|SECRET|ACCESS_TOKEN|AUTH_TOKEN|PRIVATE_KEY|DATABASE_URL|DB_PASSWORD|DB_URL|REDIS_URL|PASSWORD|TOKEN)\s*[=:]\s*\S+`), "[REDACTED CREDENTIAL]"},
}

var sensitiveJSONKey = regexp.MustCompile(`(?i)(^|[_-])(api[_-]?key|api[_-]?secret|secret[_-]?key|secret|access[_-]?token|auth[_-]?token|private[_-]?key|database[_-]?url|db[_-]?password|db[_-]?url|redis[_-]?url|password|token|jwt)($|[_-])`)

const redactedCredential = "[REDACTED CREDENTIAL]"

// maxRedactDepth bounds the walk in redactJSONValue. Tool inputs are decoded
// from daemon-supplied JSON, so nesting depth is attacker-influenced; without a
// bound a pathologically nested payload would recurse until the stack blows and
// take the process down. Real tool inputs nest a handful of levels at most, so
// this only ever trips on abuse.
const maxRedactDepth = 32

// depthLimitPlaceholder replaces anything below maxRedactDepth. Returning the
// raw value there would hand back an unscrubbed string, which is exactly what
// this package exists to prevent, so the fail-safe direction is to drop it.
const depthLimitPlaceholder = "[REDACTED DEPTH LIMIT]"

// InputMap returns a copy of m with every string value passed through Text —
// including strings nested inside maps, slices, []string and map[string]string
// — any secret-bearing key replaced wholesale, and any nested custom_env object
// replaced by coarse metadata. Non-string values are preserved unless they sit
// under a secret-bearing key.
//
// The nested walk is load-bearing, not defensive tidying: providers record
// structured tool inputs, and Codex records a file edit as
// changes[]{path, diff, content}. A top-level-only pass leaves a credential
// inside a patch body — or the full contents of a deleted .env — untouched on
// its way to the database and the WebSocket broadcast.
//
// Composites are copied rather than scrubbed in place: the caller still holds
// the original map and keeps using it after redaction (the daemon handler logs
// and re-reads it), so mutating through the shared reference would be a
// surprise at a distance.
func InputMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	redacted, _ := redactJSONMap(m, 0)
	if out, ok := redacted.(map[string]any); ok {
		return out
	}
	return map[string]any{}
}

// homeDir is resolved once at init for path redaction.
var homeDir string
var username string

func init() {
	homeDir, _ = os.UserHomeDir()
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
}

// Text scans the input string for known secret patterns and replaces
// matches with safe placeholders. It also masks the local user's home
// directory path to prevent leaking the username.
func Text(s string) string {
	s = redactStructuredJSON(s)
	return redactPlainText(s)
}

func redactPlainText(s string) string {
	for _, p := range patterns {
		s = p.re.ReplaceAllString(s, p.replacement)
	}

	// Redact home directory paths (e.g. /Users/john/ → /Users/****/).
	if homeDir != "" && username != "" {
		masked := strings.Replace(homeDir, username, "****", 1)
		s = strings.ReplaceAll(s, homeDir, masked)
	}

	return s
}

func redactStructuredJSON(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return s
	}

	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()

	var value any
	if err := dec.Decode(&value); err != nil {
		return s
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return s
	}

	redacted, changed := redactJSONValue(value, 0)
	if !changed {
		return s
	}
	out, err := json.Marshal(redacted)
	if err != nil {
		return s
	}
	return string(out)
}

// redactJSONValue scrubs a single decoded JSON value, recursing through the
// composite shapes json.Unmarshal produces plus []string and map[string]string,
// which providers use for argv- and env-style inputs.
func redactJSONValue(value any, depth int) (any, bool) {
	if depth >= maxRedactDepth {
		return depthLimitPlaceholder, true
	}
	switch v := value.(type) {
	case map[string]any:
		return redactJSONMap(v, depth)
	case []any:
		out := make([]any, len(v))
		changed := false
		for i, item := range v {
			redacted, itemChanged := redactJSONValue(item, depth+1)
			out[i] = redacted
			changed = changed || itemChanged
		}
		return out, changed
	case []string:
		out := make([]string, len(v))
		changed := false
		for i, item := range v {
			out[i] = redactPlainText(item)
			changed = changed || out[i] != item
		}
		return out, changed
	case map[string]string:
		out := make(map[string]string, len(v))
		changed := false
		for k, item := range v {
			if sensitiveJSONKey.MatchString(k) {
				out[k] = redactedCredential
				changed = true
				continue
			}
			out[k] = redactPlainText(item)
			changed = changed || out[k] != item
		}
		return out, changed
	case string:
		redacted := redactPlainText(v)
		return redacted, redacted != v
	default:
		return value, false
	}
}

func redactJSONMap(m map[string]any, depth int) (any, bool) {
	if depth >= maxRedactDepth {
		return map[string]any{"_": depthLimitPlaceholder}, true
	}
	out := make(map[string]any, len(m))
	changed := false
	customEnvSeen := false
	customEnvKeyCount := 0

	for k, v := range m {
		if strings.EqualFold(k, "custom_env") {
			customEnvSeen = true
			customEnvKeyCount = countObjectKeys(v)
			changed = true
			continue
		}

		if sensitiveJSONKey.MatchString(k) {
			out[k] = redactedCredential
			changed = true
			continue
		}

		redacted, valueChanged := redactJSONValue(v, depth+1)
		out[k] = redacted
		changed = changed || valueChanged
	}

	if customEnvSeen {
		out["has_custom_env"] = customEnvKeyCount > 0
		out["custom_env_key_count"] = customEnvKeyCount
		out["custom_env_redacted"] = true
	}

	return out, changed
}

func countObjectKeys(value any) int {
	if m, ok := value.(map[string]any); ok {
		return len(m)
	}
	return 0
}
