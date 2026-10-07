// Package guard is the policy of the smart-approve-lancet extension: dangerous-command analysis, protected paths,
// the approval settings file and the gate that turns them into allow, ask or block decisions. It ports
// smart-approve-lancet's src/{behaviors,paths}.ts and the harness-guard extension of zed-pi-harness, and has no
// dependency on the PiG SDK.
package guard

import (
	"fmt"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
)

// jsSpace lists the code points JavaScript's `\s` matches. regexp2's ECMAScript mode matches `\s` against ASCII
// only, so patterns are rewritten to use this list: a command separated by U+00A0 must read the same here as in
// the TypeScript policy.
const jsSpace = `\t\n\v\f\r \u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\ufeff`

// matchTimeout bounds one match. A pattern that runs away is an error and callers fail closed.
const matchTimeout = 2 * time.Second

// expandSpace rewrites `\s` and `\S` in a JavaScript pattern into explicit classes, inside and outside bracket
// expressions. Everything else is left alone.
func expandSpace(source string) string {
	var out strings.Builder
	inClass := false
	for i := 0; i < len(source); i++ {
		c := source[i]
		switch {
		case c == '\\' && i+1 < len(source):
			next := source[i+1]
			i++
			switch {
			case next == 's' && inClass:
				out.WriteString(jsSpace)
			case next == 's':
				out.WriteString("[" + jsSpace + "]")
			case next == 'S' && !inClass:
				out.WriteString("[^" + jsSpace + "]")
			case next == 'S':
				panic("guard: \\S inside a character class is not supported: " + source)
			default:
				out.WriteByte('\\')
				out.WriteByte(next)
			}
		case c == '[' && !inClass:
			inClass = true
			out.WriteByte(c)
		case c == ']' && inClass:
			inClass = false
			out.WriteByte(c)
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}

// jsRegex compiles a JavaScript regular expression (no flags other than i) with ECMAScript semantics.
func jsRegex(source string, ignoreCase bool) *regexp2.Regexp {
	options := regexp2.RegexOptions(regexp2.ECMAScript)
	if ignoreCase {
		options |= regexp2.IgnoreCase
	}
	re := regexp2.MustCompile(expandSpace(source), options)
	re.MatchTimeout = matchTimeout
	return re
}

// matches reports whether re matches anywhere in text; a timeout is an error.
func matches(re *regexp2.Regexp, text string) (bool, error) {
	ok, err := re.MatchString(text)
	if err != nil {
		return false, fmt.Errorf("pattern %s: %w", re.String(), err)
	}
	return ok, nil
}

// isJSSpaceRune is true for the code points JavaScript's `\s` matches.
func isJSSpaceRune(r rune) bool {
	switch {
	case r >= '\t' && r <= '\r', r == ' ', r == 0xa0, r == 0x1680, r >= 0x2000 && r <= 0x200a,
		r == 0x2028, r == 0x2029, r == 0x202f, r == 0x205f, r == 0x3000, r == 0xfeff:
		return true
	}
	return false
}
