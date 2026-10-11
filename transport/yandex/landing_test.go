package yandex

import "testing"

func TestPublicLandingLoginRequiresStructuredRootFlag(t *testing.T) {
	for _, fixture := range []struct {
		html string
		want bool
	}{
		{`<script id="store-prefetch">{"rootResourceId":"root","resources":{"root":{"meta":{"isMandatoryLoginPassthrough":true}}}}</script>`, true},
		{`<script id='store-prefetch'>{"rootResourceId":"root","resources":{"root":{"meta":{"isMandatoryLoginPassthrough":false}}}}</script>`, false},
		{`<script id="store-prefetch">{"rootResourceId":"root","resources":{"other":{"meta":{"isMandatoryLoginPassthrough":true}}}}</script>`, false},
		{`<a href="https://passport.yandex.ru/auth">Login</a>`, false},
		{`<script id="store-prefetch">{"rootResourceId":"root","resources":{"root":{"meta":{"isMandatoryLoginPassthrough":true}}}}</script><script id="client-config">{}</script>`, false},
		{`<script id="store-prefetch">not-json</script>`, false},
	} {
		if publicLandingNeedsLogin([]byte(fixture.html)) != fixture.want {
			t.Fatal("incorrect login classification")
		}
	}
}
