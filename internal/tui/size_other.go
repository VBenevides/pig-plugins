//go:build !(linux || darwin || freebsd || netbsd || openbsd)

package tui

// TerminalRows returns 0 (unknown) where the terminal size is not read; callers use a conservative default.
func TerminalRows() int { return 0 }
