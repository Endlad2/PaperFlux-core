package mailru

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"universal-bypass-tool/transport"
)

func TestMailruFailureDetailsNeverEmitProviderText(t *testing.T) {
	for _, tc := range []struct {
		err    error
		reason string
		status int
	}{
		{&mailruMetadataHTTPError{Status: 502, RetryAfter: "private-token"}, "http_status", 502},
		{context.DeadlineExceeded, "timeout", 0}, {context.Canceled, "cancelled", 0},
		{&mailruVerificationError{RetryAfter: time.Minute}, "verification_limited", 429},
		{&json.SyntaxError{}, "invalid_json", 0},
		{fmt.Errorf("Mail.ru API returned incomplete editor metadata"), "incomplete_metadata", 0},
		{fmt.Errorf("private https://host/doc/token?cookie=secret"), "unknown", 0},
		{fmt.Errorf("private wrapper %w", context.DeadlineExceeded), "timeout", 0},
	} {
		reason, status := mailruFailureDetails(tc.err)
		if reason != tc.reason || status != tc.status {
			t.Fatalf("got %s/%d, want %s/%d", reason, status, tc.reason, tc.status)
		}
	}
}

func TestMailruUpgradeMetadataRefreshPolicy(t *testing.T) {
	for _, status := range []int{401, 403, 404, 409, 410, 502, 503, 504} {
		if !mailruRefreshMetadataAfterUpgrade(status) {
			t.Fatalf("stale metadata reused after %d", status)
		}
	}
	for _, status := range []int{0, 101, 200, 429} {
		if mailruRefreshMetadataAfterUpgrade(status) {
			t.Fatalf("unnecessary metadata reset for %d", status)
		}
	}
}

func TestMailruMetadataHttpStatusIsSafeAndTyped(t *testing.T) {
	for _, status := range []int{403, 404, 502, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Accept-Language") != mailruAcceptLanguage || r.Header.Get("User-Agent") != mailruUserAgent {
				t.Error("browser headers missing")
			}
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(status)
			fmt.Fprint(w, "private-document-token")
		}))
		tr := NewMailruDocsTransport("test/doc", transport.DefaultConfig())
		_, err := tr.fetchDocInfoFrom(&http.Client{Jar: tr.cookieJar, Timeout: time.Second}, server.URL, "test/doc")
		server.Close()
		reason, got := mailruFailureDetails(err)
		if reason != "http_status" || got != status || strings.Contains(err.Error(), "private-document-token") {
			t.Fatal("missing/unsafe HTTP diagnostic")
		}
	}
}

func TestMailruBrowserHeadersAcrossWebSocketUpgrade(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != mailruUserAgent || r.Header.Get("Accept-Language") != mailruAcceptLanguage {
			t.Error("browser headers missing on upgrade")
		}
		w.WriteHeader(409)
	}))
	defer server.Close()
	headers := http.Header{}
	setMailruBrowserHeaders(headers)
	dialer := newMailruWebSocketDialer()
	_, resp, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), headers)
	if err == nil || resp == nil || resp.StatusCode != 409 {
		t.Fatal("fixture failed")
	}
	resp.Body.Close()
}
