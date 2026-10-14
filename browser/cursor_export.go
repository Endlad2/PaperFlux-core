package browser

// CursorScript is the JS hook installed by the desktop and mobile bridges.
// It is exported so platform backends can inject it without duplicating the
// string. The script is idempotent: installing it twice is a no-op.
func CursorScript() string { return cursorScript }
