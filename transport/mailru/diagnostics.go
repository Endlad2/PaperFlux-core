package mailru

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
)

type mailruMetadataHTTPError struct {
	Status     int
	RetryAfter string
}

func (e *mailruMetadataHTTPError) Error() string {
	return fmt.Sprintf("API returned status %d", e.Status)
}

// A stale editor address can return 404 while the public document still exists.
// Invalidate only metadata, not cookies, keys, or the owner's document.
func mailruRefreshMetadataAfterUpgrade(status int) bool {
	return status == 401 || status == 403 || status == 404 || status == 409 || status == 410 || status == 502 || status == 503 || status == 504
}

// Emit only enumerated categories/statuses; provider errors may contain signed
// document URLs, tokens, cookies and arbitrary user-controlled text.
func mailruFailureDetails(err error) (string, int) {
	var status *mailruMetadataHTTPError
	var verification *mailruVerificationError
	var network net.Error
	var syntax *json.SyntaxError
	var valueType *json.UnmarshalTypeError
	switch {
	case errors.As(err, &status):
		return "http_status", status.Status
	case errors.As(err, &verification):
		return "verification_limited", 429
	case errors.Is(err, context.Canceled):
		return "cancelled", 0
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &network) && network.Timeout():
		return "timeout", 0
	case errors.As(err, &syntax), errors.As(err, &valueType):
		return "invalid_json", 0
	}
	if err == nil {
		return "none", 0
	}
	known := map[string]string{
		"Mail.ru API timed out":                             "timeout",
		"Mail.ru API connection failed":                     "connection_failed",
		"Mail.ru API redirect rejected":                     "redirect_rejected",
		"Mail.ru API response could not be read":            "response_read_failed",
		"Mail.ru API response too large":                    "response_too_large",
		"invalid Mail.ru API request":                       "invalid_request",
		"document object missing":                           "missing_document",
		"editorConfig object missing":                       "missing_editor_config",
		"Mail.ru API returned an invalid editor endpoint":   "invalid_editor_endpoint",
		"Mail.ru API returned an untrusted editor endpoint": "untrusted_editor_endpoint",
		"Mail.ru API returned incomplete editor metadata":   "incomplete_metadata",
		"mailru: Socket.IO connection rejected":             "socket_rejected",
	}
	if reason, ok := known[err.Error()]; ok {
		return reason, 0
	}
	// Socket.IO helpers wrap local errors; never surface the wrapped raw text.
	if errors.Unwrap(err) != nil {
		return mailruFailureDetails(errors.Unwrap(err))
	}
	return "unknown", 0
}
