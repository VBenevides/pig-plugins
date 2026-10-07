package guard

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// DefaultProtectedPaths are the glob patterns of files that need confirmation to modify, from smart-approve-lancet's
// src/paths.ts. A leading "!" re-allows a path; the last matching pattern wins.
var DefaultProtectedPaths = []string{
	".env",
	".env.*",
	"!.env.example",
	"**/.ssh/**",
	"**/.ssh/*",
	"**/.kube/config",
	"**/.aws/credentials",
	"**/.aws/config",
	"**/.config/gh/hosts.yml",
	"**/.config/gcloud/**",
	"**/.git-credentials",
	"**/.netrc",
	"**/.npmrc",
	"**/.pypirc",
	"**/id_rsa",
	"**/id_ed25519",
	"**/*.pem",
	"**/*.key",
	"**/*.p12",
	"**/*.kdbx",
	"**/auth.json",
}

type compiledPattern struct {
	re     *regexp.Regexp
	negate bool
}

// globToRegexp converts a glob to a regular expression that is anchored at the end only, as the original is: `.env`
// therefore matches any path that ends in `.env`. `.` here also matches line breaks, which the JavaScript original
// does not, so a path with a line break in a directory name cannot slip past a `**` pattern.
func globToRegexp(pattern string) compiledPattern {
	negate := strings.HasPrefix(pattern, "!")
	pattern = strings.TrimPrefix(pattern, "!")
	var re strings.Builder
	re.WriteString("(?s)")
	for i := 0; i < len(pattern); {
		c := pattern[i]
		switch {
		case c == '*' && i+1 < len(pattern) && pattern[i+1] == '*':
			re.WriteString(".*")
			i += 2
			if i < len(pattern) && pattern[i] == '/' {
				i++
			}
		case c == '*':
			re.WriteString("[^/]*")
			i++
		case c == '?':
			re.WriteString("[^/]")
			i++
		default:
			re.WriteString(regexp.QuoteMeta(string(c)))
			i++
		}
	}
	re.WriteString("$")
	return compiledPattern{re: regexp.MustCompile(re.String()), negate: negate}
}

// PathMatcher decides whether a file path is protected. It is immutable after construction.
type PathMatcher struct {
	patterns []compiledPattern
}

// NewPathMatcher compiles glob patterns.
func NewPathMatcher(patterns []string) *PathMatcher {
	m := &PathMatcher{patterns: make([]compiledPattern, len(patterns))}
	for i, p := range patterns {
		m.patterns[i] = globToRegexp(p)
	}
	return m
}

// DefaultPathMatcher matches DefaultProtectedPaths.
func DefaultPathMatcher() *PathMatcher { return NewPathMatcher(DefaultProtectedPaths) }

// IsProtected reports whether filePath, its resolved symlink target or its absolute form matches a protected
// pattern. Each candidate is tested whole and by base name.
func (m *PathMatcher) IsProtected(filePath string) bool {
	if filePath == "" || len(m.patterns) == 0 {
		return false
	}
	candidates := []string{filePath}
	if real, err := filepath.EvalSymlinks(filePath); err == nil && real != filePath {
		candidates = append(candidates, real)
	}
	// A path that does not exist yet (a write target) is matched on its literal form below.
	if abs, err := filepath.Abs(filePath); err == nil && !slices.Contains(candidates, abs) {
		candidates = append(candidates, abs)
	}
	for _, candidate := range candidates {
		normalized := strings.ReplaceAll(candidate, `\`, "/")
		base := filepath.Base(candidate)
		matched := false
		for _, p := range m.patterns {
			if p.re.MatchString(normalized) || p.re.MatchString(base) {
				matched = !p.negate
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// RealTarget returns the real path of the deepest existing ancestor of absolute plus the not-yet-existing tail, so a
// symlinked directory cannot hide a protected destination. A dangling symlink is followed to the path a write
// through it would create. It returns "" when no ancestor can be resolved.
func RealTarget(absolute string) string { return realTarget(absolute, 0) }

// maxLinkDepth bounds how many dangling symlinks RealTarget follows.
const maxLinkDepth = 40

func realTarget(absolute string, depth int) string {
	existing := absolute
	var tail []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return ""
		}
		tail = append([]string{filepath.Base(existing)}, tail...)
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		// The node exists but does not resolve: a dangling symlink. A write through it creates its target.
		if depth >= maxLinkDepth {
			return ""
		}
		link, linkErr := os.Readlink(existing)
		if linkErr != nil {
			return ""
		}
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(existing), link)
		}
		return realTarget(filepath.Join(append([]string{link}, tail...)...), depth+1)
	}
	return filepath.Join(append([]string{real}, tail...)...)
}
