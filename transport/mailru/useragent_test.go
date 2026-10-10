package mailru

import (
	"strings"
	"testing"
)

func TestMailruUserAgentDoesNotClaimChrome(t *testing.T) {
	if !strings.Contains(mailruUserAgent, "Firefox/") || strings.Contains(mailruUserAgent, "Chrome/") {
		t.Fatal("Mail.ru agent must retain the verified Firefox gateway compatibility")
	}
}
