package browser

// ExtractCursorPayloadForTest exposes the unexported parser to the tests
// package without making it part of the public API.
func ExtractCursorPayloadForTest(text string) (string, bool) {
	return extractCursorPayload(text)
}
