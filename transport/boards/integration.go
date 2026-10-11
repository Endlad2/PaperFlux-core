package boards

import (
	"context"
	"net/http"
	"universal-bypass-tool/transport"
)

var _ transport.ErrorNotifier = (*BoardsTransport)(nil)

func (t *BoardsTransport) scopedAPIRequest(hash, action string, content any) *http.Request {
	r := apiRequest(hash, action, content)
	t.jarMu.RLock()
	ctx := t.authCtx
	t.jarMu.RUnlock()
	if ctx == nil {
		ctx = context.Background()
	}
	return r.WithContext(ctx)
}
