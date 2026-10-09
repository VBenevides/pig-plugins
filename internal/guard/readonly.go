package guard

import (
	"os"
	"strings"
)

// readOnlyCommand recognizes a deliberately small shell subset, not arbitrary
// shell programs. Unrecognized syntax falls back to the existing guard policy.
// Only literal arguments and read-only pipelines/command lists qualify.
func readOnlyCommand(command string) bool {
	if len(command) == 0 || len(command) > 64*1024 {
		return false
	}
	var words []string
	var word strings.Builder
	var quote byte
	inWord := false
	flush := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				// Single quotes are literal. Double quotes can execute expansions.
				if quote == '"' && strings.ContainsRune("$`\\", rune(ch)) {
					return false
				}
				word.WriteByte(ch)
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
			inWord = true
		case ' ', '\t':
			flush()
		case ';', '|', '&', '\n':
			flush()
			if !readOnlyWords(words) {
				return false
			}
			words = nil
			if ch == '&' {
				if i+1 >= len(command) || command[i+1] != '&' {
					return false // Background execution is not in this subset.
				}
				i++
			} else if ch == '|' && i+1 < len(command) && command[i+1] == '|' {
				i++
			}
		case '$', '`', '\\', '<', '>', '(', ')', '{', '}', '#', '!', '\r':
			return false
		default:
			if ch < 32 || ch == 127 {
				return false
			}
			// An unqualified glob can expand to an executable-hook option
			// such as --pre. Path-prefixed globs cannot produce option words.
			if strings.ContainsRune("*?[", rune(ch)) && (!strings.Contains(word.String(), "/") || strings.HasPrefix(word.String(), "-")) {
				return false
			}
			word.WriteByte(ch)
			inWord = true
		}
	}
	flush()
	return quote == 0 && readOnlyWords(words)
}

func readOnlyWords(words []string) bool {
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "readlink", "strings", "head", "tail", "cat", "ls", "pwd", "wc", "stat", "grep":
		return true
	case "rg":
		// A user config can supply --pre without it appearing in the command.
		if os.Getenv("RIPGREP_CONFIG_PATH") != "" {
			return false
		}
		for _, arg := range words[1:] {
			if arg == "--pre" || strings.HasPrefix(arg, "--pre=") || arg == "--hostname-bin" || strings.HasPrefix(arg, "--hostname-bin=") {
				return false // These options execute external programs.
			}
		}
		return true
	default:
		return false
	}
}
