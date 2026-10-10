package mailru

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// Expiry is only a conservative cache limit, never an authentication decision.
// Unsigned/opaque/malformed tokens are not cached. The server validates JWTs.
func mailruMetadataCacheDeadline(token string, now time.Time) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(parts[1]) > 65536 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claim struct {
		Expires int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claim) != nil || claim.Expires <= 0 {
		return time.Time{}
	}
	expiry := time.Unix(claim.Expires, 0).Add(-30 * time.Second)
	if !expiry.After(now) {
		return time.Time{}
	}
	return minTime(expiry, now.Add(time.Minute))
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (t *MailruDocsTransport) invalidateMetadata() {
	t.cacheMu.Lock()
	t.cachedInfo = MailruDocsInfo{}
	t.cacheUntil = time.Time{}
	t.cacheMu.Unlock()
}
