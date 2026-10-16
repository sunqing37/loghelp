package support

import (
	"os"
	"regexp"
	"strings"
)

var redactions = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization|proxy-authorization|cookie|set-cookie)\s*[:=][^\r\n]*`),
	regexp.MustCompile(`(?i)(password|passwd|token|secret|api[_-]?key|pairing[_-]?code|配对码|密码)\s*["']?\s*[:=]\s*[^\s,;]+`),
	regexp.MustCompile(`(?i)\b(?:https?|socks5h?)://[^\s<>"']+`),
	regexp.MustCompile(`(?i)\bBearer\s+[^\s,;]+`),
	regexp.MustCompile(`[A-Za-z]:[\\/][^\r\n\t"<>|]*`),
	regexp.MustCompile(`(?:/Users/|/home/|/tmp/|/var/|/private/)[^\r\n\t"<>]*`),
	regexp.MustCompile(`\\\\[^\r\n\t"<>]+`),
	regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`),
	regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}(?::[0-9]+)?\b`),
	regexp.MustCompile(`(?i)\b(?:[0-9a-f]{0,4}:){2,}[0-9a-f:%]*\b`),
	regexp.MustCompile(`\b[0-9]{6}\b`),
}

// Redact is a best-effort scrubber for local messages and user-authored feedback.
// Exported automatic diagnostics use allowlists instead of trusting this filter.
func Redact(value string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		value = strings.ReplaceAll(value, home, "[用户目录]")
		value = strings.ReplaceAll(value, strings.ReplaceAll(home, `\`, "/"), "[用户目录]")
	}
	for _, pattern := range redactions {
		value = pattern.ReplaceAllString(value, "[已脱敏]")
	}
	return strings.ToValidUTF8(value, "�")
}
