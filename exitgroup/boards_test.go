package exitgroup

import (
	"strings"
	"testing"
	"time"
)

func TestBoardManifestResources(t *testing.T) {
	now := time.Now()
	for typ, resource := range map[string]string{"boards": "https://boards.yandex.ru/whiteboard/?hash=fixture", "mtslink": "https://boards.mts-link.ru/board/fixture"} {
		m := Manifest{Version: 1, ValidUntil: now.Add(time.Minute).Unix(), Profiles: []Profile{{ID: "42", Token: strings.Repeat("a", 32), ClientIP: "10.10.10.2", Transport: typ, Documents: []string{resource}}}}
		if e := m.Validate(now); e != nil {
			t.Fatal(e)
		}
		m.Profiles[0].Documents = append(m.Profiles[0].Documents, resource)
		if m.Validate(now) == nil {
			t.Fatal("multiple boards accepted")
		}
		m.Profiles[0].Documents = []string{"https://evil.example/board/fixture"}
		if m.Validate(now) == nil {
			t.Fatal("untrusted board accepted")
		}
	}
}
