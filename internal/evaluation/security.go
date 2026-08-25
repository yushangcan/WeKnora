package evaluation

import (
	"regexp"
	"strings"
)

const evaluationErrorMessageMaxRunes = 1024

var evaluationErrorSecretPatterns = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{
		pattern:     regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+)[^\s,;]+`),
		replacement: `${1}[REDACTED]`,
	},
	{
		pattern: regexp.MustCompile(
			`(?i)((?:api[_-]?key|app[_-]?secret|access[_-]?token|refresh[_-]?token|token|secret)\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;]+)`,
		),
		replacement: `${1}[REDACTED]`,
	},
	{
		pattern:     regexp.MustCompile(`(?i)(https?://)[^/\s:@]+:[^/\s@]+@`),
		replacement: `${1}[REDACTED]@`,
	},
}

// SafeErrorMessage removes common credentials and bounds persisted error text.
func SafeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return SafeErrorText(err.Error())
}

// SafeErrorText sanitizes text supplied by recovery and repository callers.
func SafeErrorText(message string) string {
	message = strings.Map(func(r rune) rune {
		if r < 32 {
			return ' '
		}
		return r
	}, message)
	for _, secretPattern := range evaluationErrorSecretPatterns {
		message = secretPattern.pattern.ReplaceAllString(message, secretPattern.replacement)
	}
	runes := []rune(strings.TrimSpace(message))
	if len(runes) > evaluationErrorMessageMaxRunes {
		return string(runes[:evaluationErrorMessageMaxRunes]) + "..."
	}
	return string(runes)
}
