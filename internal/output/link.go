package output

import "fmt"

// Hyperlink wraps text in an OSC 8 escape sequence pointing at url. Terminals
// that support OSC 8 (iTerm2, WezTerm, Kitty, Alacritty, VS Code, recent
// gnome-terminal) render text as clickable; others typically ignore the
// escape. Caller should still gate on a TTY check.
func Hyperlink(url, text string) string {
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, text)
}

// PkgDevURL returns the canonical pkg.go.dev page for a module path.
func PkgDevURL(modulePath string) string {
	return "https://pkg.go.dev/" + modulePath
}
