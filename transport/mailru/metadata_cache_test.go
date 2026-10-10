package mailru

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"
	"universal-bypass-tool/transport"
)

func TestMetadataCacheConservativeExpiryAndInvalidation(t *testing.T) {
	now := time.Unix(1800000000, 0)
	token := func(exp int64) string {
		return "header." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp))) + ".signature"
	}
	for _, item := range []string{"opaque", "a.invalid.c", token(now.Unix() - 1), token(now.Unix() + 20)} {
		if !mailruMetadataCacheDeadline(item, now).IsZero() {
			t.Fatal("unknown/expired token cached")
		}
	}
	if got := mailruMetadataCacheDeadline(token(now.Unix()+300), now); !got.Equal(now.Add(time.Minute)) {
		t.Fatal("cache exceeds minute")
	}
	if got := mailruMetadataCacheDeadline(token(now.Unix()+45), now); !got.Equal(now.Add(15 * time.Second)) {
		t.Fatal("expiry safety margin lost")
	}
	tr := NewMailruDocsTransport("test/doc", transport.DefaultConfig())
	tr.cachedInfo = MailruDocsInfo{Token: "private"}
	tr.cacheUntil = now.Add(time.Minute)
	tr.invalidateMetadata()
	if !tr.cacheUntil.IsZero() || tr.cachedInfo.Token != "" {
		t.Fatal("rejected metadata retained")
	}
}
