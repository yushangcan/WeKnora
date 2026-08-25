package evaluation

import (
	"errors"
	"strings"
	"testing"
)

func TestSafeErrorMessageRedactsCredentialsAndBoundsText(t *testing.T) {
	err := errors.New(
		"provider failed\nAuthorization: Bearer bearer-secret api_key=api-secret " +
			"token:'token-secret' endpoint=https://user:password@example.com " +
			strings.Repeat("x", evaluationErrorMessageMaxRunes+100),
	)
	message := SafeErrorMessage(err)
	for _, secret := range []string{"bearer-secret", "api-secret", "token-secret", "user:password"} {
		if strings.Contains(message, secret) {
			t.Fatalf("safe error contains secret %q: %s", secret, message)
		}
	}
	if strings.ContainsAny(message, "\r\n\t") {
		t.Fatalf("safe error contains control whitespace: %q", message)
	}
	if len([]rune(message)) > evaluationErrorMessageMaxRunes+3 {
		t.Fatalf("safe error is too long: %d", len([]rune(message)))
	}
}
