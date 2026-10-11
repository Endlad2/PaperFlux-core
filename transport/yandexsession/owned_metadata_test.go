package yandex

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"universal-bypass-tool/transport"
)

func TestOwnedDocumentMetadata(t *testing.T) {
	raw := os.Getenv("PAPERFLUX_OWNED_URL")
	if raw == "" {
		t.Skip("explicit owned fixture required")
	}
	tr := NewYandexDocsTransport(raw, transport.DefaultConfig())
	var cookies map[string]string
	_ = json.Unmarshal([]byte(os.Getenv("PAPERFLUX_OWNED_COOKIES")), &cookies)
	if e := tr.ApplyCookies(cookies); e != nil {
		t.Fatal("fixture cookies rejected")
	}
	_, err := tr.fetchDocInfo(raw, "owned-metadata-probe")
	if err == nil {
		t.Log("document_status=editor-configuration-available")
		return
	}
	if errors.Is(err, ErrLoginRequired) {
		t.Log("document_status=explicit-login-required; no editor session established")
		return
	}
	if errors.Is(err, ErrCaptchaRequired) {
		t.Log("document_status=captcha-required; no editor session established")
		return
	}
	t.Fatal(err)
}
