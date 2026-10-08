package betterfooter

import (
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// toFixed formats v with d decimals like JavaScript's Number.prototype.toFixed: the exact binary value is rounded
// half up, so 1.25.toFixed(1) is "1.3" where Go's %.1f gives "1.2".
func toFixed(v float64, d int) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	negative := v < 0
	if negative {
		v = -v
	}
	// 40 fractional digits are exact for every double the footer shows, and far beyond the rounding position.
	text := strconv.FormatFloat(v, 'f', 40, 64)
	dot := strings.IndexByte(text, '.')
	digits := []byte(text[:dot] + text[dot+1:dot+1+d])
	if text[dot+1+d] >= '5' {
		i := len(digits) - 1
		for ; i >= 0; i-- {
			if digits[i] == '9' {
				digits[i] = '0'
				continue
			}
			digits[i]++
			break
		}
		if i < 0 {
			digits = append([]byte{'1'}, digits...)
		}
	}
	whole, frac := string(digits[:len(digits)-d]), string(digits[len(digits)-d:])
	out := whole
	if d > 0 {
		out += "." + frac
	}
	if negative && strings.Trim(out, "0.") != "" {
		out = "-" + out
	}
	return out
}

// round is JavaScript's Math.round: halves round up.
func round(v float64) float64 { return math.Floor(v + 0.5) }

// FormatTokens is Pi's compact token formatter: 999, 1.2k, 12k, 1.2M, 12M.
func FormatTokens(count int) string {
	n := float64(count)
	switch {
	case count < 1000:
		return strconv.Itoa(count)
	case count < 10000:
		return toFixed(n/1000, 1) + "k"
	case count < 1_000_000:
		return strconv.FormatFloat(round(n/1000), 'f', 0, 64) + "k"
	case count < 10_000_000:
		return toFixed(n/1_000_000, 1) + "M"
	}
	return strconv.FormatFloat(round(n/1_000_000), 'f', 0, 64) + "M"
}

// FormatReset is the compact time until a reset using only the largest unit: 6d, 1h, 30m, 12s.
func FormatReset(sec float64) string {
	if sec <= 0 || math.IsNaN(sec) {
		return "0s"
	}
	total := int64(math.Floor(sec))
	switch {
	case total/86400 >= 1:
		return strconv.FormatInt(total/86400, 10) + "d"
	case total/3600 >= 1:
		return strconv.FormatInt(total/3600, 10) + "h"
	case total/60 >= 1:
		return strconv.FormatInt(total/60, 10) + "m"
	}
	return strconv.FormatInt(total, 10) + "s"
}

var spaces = regexp.MustCompile(` +`)

// Sanitize turns CR, LF and tab into spaces, collapses runs of spaces and trims, as the status text of the footer.
// Other control characters are removed; ESC stays because statuses carry their own SGR styling.
func Sanitize(text string) string {
	text = strings.Map(func(r rune) rune {
		switch {
		case r == '\r' || r == '\n' || r == '\t':
			return ' '
		case r < 0x20 && r != 0x1b || r == 0x7f:
			return -1
		}
		return r
	}, text)
	return strings.TrimSpace(spaces.ReplaceAllString(text, " "))
}

// ShortenCwd replaces the home prefix of cwd with "~". A trailing separator in home would swallow the next
// separator, and a root or bare-drive home would match every path, so those are ignored.
func ShortenCwd(cwd, home string) string {
	home = strings.TrimRight(home, `/\`)
	if home == "" || len(home) == 2 && home[1] == ':' {
		return cwd
	}
	if cwd == home || strings.HasPrefix(cwd, home+string(filepath.Separator)) {
		return "~" + cwd[len(home):]
	}
	return cwd
}

var versionPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// NormalizeVersion returns "v"+version for a semantic version and "" for anything else. The anchored pattern
// keeps manifest-controlled escapes, newlines and surrounding whitespace out of the footer.
func NormalizeVersion(version string) string {
	if !versionPattern.MatchString(version) {
		return ""
	}
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}
