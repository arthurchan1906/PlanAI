package hook

import "aipmc/hook/cursor"

// ProcessCursorHook reads Cursor hook stdin JSON and saves to discussion_log.
func ProcessCursorHook() {
	cursor.ProcessHook()
}

// SetupCursorHooks installs Cursor project hooks (.cursor/hooks.json + aipm-hook.js).
func SetupCursorHooks(commandPath string) error {
	return cursor.SetupHooks(commandPath)
}
