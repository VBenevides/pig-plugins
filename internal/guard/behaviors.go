package guard

import (
	"strings"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
)

// Analysis is the result of analyzing one bash command.
type Analysis struct {
	// Behaviors are the matched behavior ids, in order of first detection.
	Behaviors []string
	// Labels are the English descriptions, one per behavior.
	Labels []string
	// HardBlocked is true when any behavior may never run, whatever the mode or the user says.
	HardBlocked bool
	// DenyTier is true for behaviors that auto-approval would deny without a model verdict.
	DenyTier bool
}

var behaviorLabels = map[string]string{
	"recursive-force-delete":   "Recursive force delete (rm -rf)",
	"delete-root":              "Delete root path /",
	"delete-sys-dir":           "Delete system directory",
	"fork-bomb":                "Fork bomb",
	"remote-fetch-exec":        "Remote fetch-and-execute (curl|sh)",
	"write-sensitive-file":     "Write to system-sensitive file",
	"write-block-device":       "Write to raw block device",
	"chmod-sys-dir":            "Change system directory permissions",
	"shutdown-reboot":          "Shutdown / reboot",
	"disk-format":              "Disk format / raw write",
	"mount-block-device":       "Mount / unmount block device",
	"force-kill":               "Force kill process (SIGKILL)",
	"pkg-global-uninstall":     "Global package uninstall",
	"git-force-push":           "git force / mirror push",
	"git-push-delete":          "git push --delete (remote ref)",
	"git-push-colon-ref":       "git push :ref (delete remote branch)",
	"git-hard-reset":           "git reset --hard (discard changes)",
	"git-clean":                "git clean -f (delete untracked)",
	"git-branch-delete":        "git branch -D (force delete)",
	"git-tag-delete":           "git tag -d (delete tag)",
	"git-stash-clear":          "git stash clear",
	"git-stash-drop":           "git stash drop",
	"git-reflog-expire":        "git reflog expire",
	"git-gc-prune":             "git gc --prune (purge objects)",
	"git-filter-branch":        "git filter-branch (rewrite history)",
	"git-filter-repo":          "git filter-repo (rewrite history)",
	"git-commit-amend":         "git commit --amend",
	"git-rebase":               "git rebase (rewrite history)",
	"git-remote-rm":            "git remote rm",
	"git-submodule-deinit":     "git submodule deinit",
	"git-worktree-remove":      "git worktree remove",
	"git-update-ref-delete":    "git update-ref -d (delete ref)",
	"git-checkout-discard":     "git checkout -- . (discard all)",
	"git-restore-discard":      "git restore . (discard worktree)",
	"git-config-global":        "git config --global",
	"git-notes-remove":         "git notes remove",
	"sudo":                     "sudo command",
	"docker-destroy":           "docker rm/rmi/volume/network rm",
	"kubectl-delete":           "kubectl delete",
	"mv-sys-dir":               "Move system directory",
	"cp-root":                  "Recursive copy to root",
	"git-force-push-protected": "git force push to protected branch",
	"delete-home":              "Delete home directory (~)",
}

var hardBlockBehaviors = map[string]bool{
	"delete-root": true, "delete-home": true, "delete-sys-dir": true, "fork-bomb": true,
	"remote-fetch-exec": true, "write-sensitive-file": true, "write-block-device": true,
	"disk-format": true, "shutdown-reboot": true,
}

var denyTierBehaviors = map[string]bool{"git-force-push-protected": true}

type dangerRule struct {
	pattern  *regexp2.Regexp
	behavior string
}

const blockDevice = `(?:sd[a-z]\d*|nvme\d|disk\d|hd[a-z]|vd[a-z]|xvd[a-z]|mmcblk|mapper\/|md\d|sg\d|st\d|nst\d)`

// dangerRules are the secondary net of regular expressions, copied verbatim from smart-approve-lancet's
// src/behaviors.ts and evaluated in this order.
var dangerRules = func() []dangerRule {
	type source struct {
		pattern    string
		behavior   string
		ignoreCase bool
	}
	sources := []source{
		{`(?:^|[\s;&|])rm\s+(?:-[a-zA-Z]*r[a-zA-Z]*f|-[a-zA-Z]*f[a-zA-Z]*r)\s+`, "recursive-force-delete", true},
		{`\brm\s+--recursive(?:\s+--force|\s*$)`, "recursive-force-delete", true},
		{`\brm\s+--force(?:\s+--recursive|\s*$)`, "recursive-force-delete", true},
		{`\brmdir\s+(?:-[a-zA-Z]*p[a-zA-Z]*)?\s*\/`, "recursive-force-delete", false},
		{`\brm\b.*\s\/(?:\s|$)`, "delete-root", false},
		{`:\(\)\s*\{\s*:\|:\s*&\s*\}\s*;:`, "fork-bomb", false},
		{`\b(?:fork|bomb)\b.*\&\s*\|.*\&`, "fork-bomb", false},
		{`\b(?:curl|wget|fetch)\b.*\|\s*(?:sudo\s+)?(?:sh|bash|zsh|fish|python|python3|perl|ruby|node)\b`, "remote-fetch-exec", false},
		{`\b(?:curl|wget)\b.*(?:\|\s*sh|\|\s*bash)`, "remote-fetch-exec", false},
		{`(?:>|>>)\s*\/(?:etc\/(?:passwd|shadow|sudoers|hosts)|proc|sys|dev\/` + blockDevice + `)`, "write-sensitive-file", false},
		{`(?:>|>>)\s*\/dev\/` + blockDevice, "write-block-device", false},
		{`\b(?:chmod|chown|chgrp)\b.*\s\/(?:etc|usr|var|bin|sbin|boot)\b`, "chmod-sys-dir", false},
		{`\b(?:shutdown|poweroff|halt|reboot|init\s+0|init\s+6)\b`, "shutdown-reboot", false},
		{`\bsudo\s+(?:shutdown|poweroff|halt|reboot|init)\b`, "shutdown-reboot", false},
		{`\b(?:mkfs|dd)\b.*(?:of=)?\/dev\/` + blockDevice, "disk-format", false},
		{`\b(?:mount|umount)\b.*\s\/dev\/(?:sd|nvme|disk)`, "mount-block-device", false},
		{`\b(?:pkill|killall)\s+(?:-[a-zA-Z]*9|-\d+)\s+`, "force-kill", false},
		{`\bkill\s+-9\b`, "force-kill", false},
		{`\b(?:npm|pnpm|yarn)\s+(?:uninstall|remove|rm)\s+(?:-[a-zA-Z]*g[a-zA-Z]*)\b`, "pkg-global-uninstall", false},
		{`\bpip3?\s+uninstall\b`, "pkg-global-uninstall", false},
		{`\bgit\s+push\b.*(?:\s-f\b|--force(?:-with-lease)?\b|--mirror\b)`, "git-force-push", false},
		{`\bgit\s+push\b.*--delete\b`, "git-push-delete", false},
		{`\bgit\s+push\s+\S+\s+:[^\s]`, "git-push-colon-ref", false},
		{`\bgit\s+reset\b.*--hard\b`, "git-hard-reset", false},
		{`\bgit\s+reset\s+-[a-zA-Z]*H`, "git-hard-reset", false},
		{`\bgit\s+clean\s+-[a-zA-Z]*f`, "git-clean", false},
		{`\bgit\s+branch\s+-[a-zA-Z]*D\b`, "git-branch-delete", false},
		{`\bgit\s+tag\s+(?:-d\b|--delete\b)`, "git-tag-delete", false},
		{`\bgit\s+stash\s+clear\b`, "git-stash-clear", false},
		{`\bgit\s+stash\s+drop\b`, "git-stash-drop", false},
		{`\bgit\s+reflog\s+expire\b`, "git-reflog-expire", false},
		{`\bgit\s+gc\b.*--prune`, "git-gc-prune", false},
		{`\bgit\s+filter-branch\b`, "git-filter-branch", false},
		{`\bgit\s+filter-repo\b`, "git-filter-repo", false},
		{`\bgit\s+commit\b.*--amend\b`, "git-commit-amend", false},
		{`\bgit\s+rebase\b(?!\s+--(?:abort|continue|skip)\b)`, "git-rebase", false},
		{`\bgit\s+remote\s+(?:rm|remove)\b`, "git-remote-rm", false},
		{`\bgit\s+submodule\s+deinit\b`, "git-submodule-deinit", false},
		{`\bgit\s+worktree\s+remove\b`, "git-worktree-remove", false},
		{`\bgit\s+update-ref\b.*(?:-d\b|--delete\b)`, "git-update-ref-delete", false},
		{`\bgit\s+(?:checkout|restore)\s+--\s*\.`, "git-checkout-discard", false},
		{`\bgit\s+restore\s+(?:\.|--worktree\b)`, "git-restore-discard", false},
		{`\bgit\s+config\b.*--global\b`, "git-config-global", false},
		{`\bgit\s+notes\b.*\bremove\b`, "git-notes-remove", false},
		{`\bsudo\s+`, "sudo", false},
		{`\bdocker\s+(?:rm|rmi|volume\s+rm|network\s+rm)\b`, "docker-destroy", false},
		{`\bkubectl\s+delete\b`, "kubectl-delete", false},
		{`\bmv\b.*\s\/(?:usr|etc|var|bin)\b`, "mv-sys-dir", false},
		{`\bcp\s+-r\b.*\s\/\s*$`, "cp-root", false},
		{`(?:^|[\s;&|])rm\s+(?:-[a-zA-Z]*r[a-zA-Z]*f|-[a-zA-Z]*f[a-zA-Z]*r)\s+(?:~|\$HOME)(?=$|[\s;&|])`, "delete-home", true},
		{`\brm\s+-rf\s+\/Users\/[^\s/;|&]+`, "delete-home", true},
	}
	rules := make([]dangerRule, len(sources))
	for i, s := range sources {
		rules[i] = dangerRule{pattern: jsRegex(s.pattern, s.ignoreCase), behavior: s.behavior}
	}
	return rules
}()

// isLineTerminator is true for the code points that JavaScript's `.` does not match.
func isLineTerminator(r rune) bool { return r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029 }

// Normalize prepares a command for stable matching: it strips a trailing comment, collapses whitespace runs and
// trims. Like the TypeScript original, `#.*$` without the multiline flag can only match in the last line, so a
// `#` on an earlier line is kept.
func Normalize(command string) string {
	tail := strings.LastIndexFunc(command, isLineTerminator)
	start := 0
	if tail >= 0 {
		_, size := utf8.DecodeRuneInString(command[tail:])
		start = tail + size
	}
	if hash := strings.IndexByte(command[start:], '#'); hash >= 0 {
		command = command[:start+hash]
	}
	var out strings.Builder
	pendingSpace := false
	for _, r := range command {
		if isJSSpaceRune(r) {
			pendingSpace = out.Len() > 0
			continue
		}
		if pendingSpace {
			out.WriteByte(' ')
			pendingSpace = false
		}
		out.WriteRune(r)
	}
	return out.String()
}

func roughTokenize(command string) []string {
	var tokens []string
	var current strings.Builder
	var quote rune
	for _, r := range command {
		switch {
		case quote != 0:
			current.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
			current.WriteRune(r)
		case isJSSpaceRune(r):
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

var stopOperators = map[string]bool{"|": true, "||": true, "&&": true, ";": true, "&": true, "(": true, ")": true, "{": true, "}": true, "<": true, ">": true}

func extractLeadingArgs(tokens []string, executable string) ([]string, bool) {
	for i, token := range tokens {
		if token != executable {
			continue
		}
		after := tokens[i+1:]
		for end, t := range after {
			if stopOperators[t] {
				return after[:end], true
			}
		}
		return after, true
	}
	return nil, false
}

func skipGitGlobalOptions(args []string) (subcommand string, rest []string, ok bool) {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		switch args[i] {
		case "-C", "--git-dir", "--work-tree", "-c":
			i += 2
		default:
			i++
		}
		if i > len(args) {
			return "", nil, false
		}
	}
	if i >= len(args) {
		return "", nil, false
	}
	return args[i], args[i+1:], true
}

func isForcePush(args []string) bool {
	for _, arg := range args {
		switch {
		case arg == "-f" || arg == "--force", arg == "--force-with-lease", strings.HasPrefix(arg, "--force-with-lease="),
			arg == "--force-if-includes":
			return true
		case strings.HasPrefix(arg, "+") && len(arg) > 1 && !strings.HasPrefix(arg, "+-"):
			return true
		}
	}
	return false
}

var protectedPushBranches = map[string]bool{"main": true, "master": true, "production": true, "prod": true, "release": true, "trunk": true}

// pushTargetBranches extracts candidate remote branch names from push args. JavaScript's string lengths count
// UTF-16 units but only the position of ASCII separators matters here, so byte offsets are equivalent.
func pushTargetBranches(args []string) []string {
	var branches []string
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		target := arg
		if strings.Contains(target, ":") {
			target = target[strings.LastIndex(target, ":")+1:]
		}
		target = strings.TrimPrefix(target, "+")
		target = target[strings.LastIndex(target, "/")+1:]
		if target != "" {
			branches = append(branches, target)
		}
	}
	return branches
}

func isBranchDelete(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "-D" || arg == "-d" || arg == "--delete" {
			return true
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsAny(arg, "dD") {
			return true
		}
	}
	return false
}

func isGitCleanDestructive(args []string) bool {
	for _, arg := range args {
		if arg == "-n" || arg == "--dry-run" {
			return false
		}
	}
	for _, arg := range args {
		if arg == "--" {
			break
		}
		switch arg {
		case "-f", "--force", "-x", "-X", "-d", "--directories":
			return true
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsAny(arg, "fxXd") {
			return true
		}
	}
	return false
}

func analyzeGit(args []string) []string {
	subcommand, rest, ok := skipGitGlobalOptions(args)
	if !ok {
		return nil
	}
	switch subcommand {
	case "push":
		if !isForcePush(rest) {
			return nil
		}
		ids := []string{"git-force-push"}
		for _, branch := range pushTargetBranches(rest) {
			if protectedPushBranches[branch] {
				ids = append(ids, "git-force-push-protected")
				break
			}
		}
		return ids
	case "branch":
		if isBranchDelete(rest) {
			return []string{"git-branch-delete"}
		}
	case "worktree":
		if len(rest) > 0 && (rest[0] == "remove" || rest[0] == "rm") {
			return []string{"git-worktree-remove"}
		}
	case "reset":
		for _, arg := range rest {
			if arg == "--hard" {
				return []string{"git-hard-reset"}
			}
		}
	case "clean":
		if isGitCleanDestructive(rest) {
			return []string{"git-clean"}
		}
	}
	return nil
}

var (
	systemDirs     = map[string]bool{"usr": true, "etc": true, "bin": true, "sbin": true, "lib": true, "lib64": true, "boot": true, "var": true, "opt": true, "srv": true, "home": true, "root": true, "users": true, "dev": true, "system": true, "system32": true, "windows": true}
	homeContainers = map[string]bool{"home": true, "users": true}
)

const homeDepth = 2

var (
	deleteTargetHead = jsRegex(`^["'`+"`"+`]*(\$\{HOME\}|\$HOME|~|\/)([\w.\-/*]*)(.*)$`, false)
	deleteTargetTail = jsRegex(`^["'`+"`"+`;,)}\]]*$`, false)
	homeUserName     = jsRegex(`^[A-Za-z_][\w.-]*$`, false)
	deleteRecursive  = jsRegex(`(?:^|\s)-[a-zA-Z]*r[a-zA-Z]*\b|--recursive\b`, false)
	deleteForce      = jsRegex(`(?:^|\s)-[a-zA-Z]*f[a-zA-Z]*\b|--force\b`, false)
	rmWord           = jsRegex(`\brm\b`, false)
)

// NormalizeDeleteTarget resolves root, home and system aliases of a delete target: "root", "home", "system"
// or "other". The head expression uses the `s` flag in the original, so `.` here also matches line breaks.
func NormalizeDeleteTarget(raw string) (string, error) {
	// RegexOptions.Singleline is not available in ECMAScript mode, so line breaks are made visible to `.` by
	// matching on a copy with them replaced; the tail check then still sees them as non-matching characters.
	head, err := matchGroups(deleteTargetHead, replaceLineBreaks(raw))
	if err != nil {
		return "", err
	}
	if head == nil {
		return "other", nil
	}
	tail := head[3]
	// The original tests the real tail; a replaced line break is not in the allowed class either way.
	ok, err := matches(deleteTargetTail, tail)
	if err != nil {
		return "", err
	}
	if !ok {
		return "other", nil
	}
	base := "home"
	if head[1] == "/" {
		base = "root"
	}
	rest := head[2]
	if head[1] == "~" {
		ok, err := matches(homeUserName, rest)
		if err != nil {
			return "", err
		}
		if ok {
			return "home", nil
		}
	}
	const home = "\x00home\x00"
	var parts []string
	if base == "home" {
		for range homeDepth {
			parts = append(parts, home)
		}
	}
	for _, part := range strings.Split(rest, "/") {
		switch part {
		case "", ".", "*":
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		default:
			parts = append(parts, part)
		}
	}
	switch {
	case len(parts) == 0:
		return "root", nil
	case len(parts) == homeDepth && parts[0] == home && parts[1] == home:
		return "home", nil
	case len(parts) == homeDepth && homeContainers[strings.ToLower(parts[0])]:
		return "home", nil
	case len(parts) < homeDepth && parts[0] == home:
		return "system", nil
	case len(parts) == 1 && systemDirs[strings.ToLower(parts[0])]:
		return "system", nil
	}
	return "other", nil
}

func replaceLineBreaks(s string) string {
	if !strings.ContainsAny(s, "\n\r\u2028\u2029") {
		return s
	}
	return strings.Map(func(r rune) rune {
		if isLineTerminator(r) {
			return '\x01'
		}
		return r
	}, s)
}

// matchGroups returns the capture groups of the first match (index 0 is the whole match), or nil.
func matchGroups(re *regexp2.Regexp, text string) ([]string, error) {
	m, err := re.FindStringMatch(text)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	groups := m.Groups()
	out := make([]string, len(groups))
	for i := range groups {
		out[i] = groups[i].String()
	}
	return out, nil
}

// recursiveDeleteTargets returns the non-flag words of every `rm` that has both a recursive and a force flag.
func recursiveDeleteTargets(command string) ([]string, error) {
	var targets []string
	m, err := rmWord.FindStringMatch(command)
	for ; err == nil && m != nil; m, err = rmWord.FindNextMatch(m) {
		// regexp2 reports rune offsets.
		after := string([]rune(command)[m.Index+m.Length:])
		if end := strings.IndexAny(after, "|;&\n"); end >= 0 {
			after = after[:end]
		}
		for _, check := range []*regexp2.Regexp{deleteRecursive, deleteForce} {
			ok, matchErr := matches(check, after)
			if matchErr != nil {
				return nil, matchErr
			}
			if !ok {
				after = ""
				break
			}
		}
		if after == "" {
			continue
		}
		for _, token := range splitJSSpace(after) {
			if token != "" && !strings.HasPrefix(token, "-") {
				targets = append(targets, token)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return targets, nil
}

// splitJSSpace splits on runs of JavaScript whitespace, keeping the empty strings at either end like
// String.prototype.split(/\s+/).
func splitJSSpace(s string) []string {
	var parts []string
	start := 0
	inSpace := false
	for i, r := range s {
		if isJSSpaceRune(r) {
			if !inSpace {
				parts = append(parts, s[start:i])
				inSpace = true
			}
			start = i + utf8.RuneLen(r)
			continue
		}
		inSpace = false
	}
	return append(parts, s[start:])
}

// Analyze checks a bash command for dangerous behaviors by combining git argument parsing, delete-target
// normalization and the regular-expression rules. An error means a pattern could not be evaluated (a timeout);
// callers fail closed.
func Analyze(command string) (Analysis, error) {
	c := Normalize(command)
	var behaviors []string
	seen := map[string]bool{}
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			behaviors = append(behaviors, id)
		}
	}

	if args, ok := extractLeadingArgs(roughTokenize(c), "git"); ok {
		for _, id := range analyzeGit(args) {
			add(id)
		}
	}
	targets, err := recursiveDeleteTargets(c)
	if err != nil {
		return Analysis{}, err
	}
	for _, target := range targets {
		kind, err := NormalizeDeleteTarget(target)
		if err != nil {
			return Analysis{}, err
		}
		switch kind {
		case "root":
			add("delete-root")
		case "home":
			add("delete-home")
		case "system":
			add("delete-sys-dir")
		}
	}
	for _, rule := range dangerRules {
		ok, err := matches(rule.pattern, c)
		if err != nil {
			return Analysis{}, err
		}
		if ok {
			add(rule.behavior)
		}
	}

	analysis := Analysis{Behaviors: behaviors, Labels: make([]string, len(behaviors))}
	for i, id := range behaviors {
		label, known := behaviorLabels[id]
		if !known {
			label = id
		}
		analysis.Labels[i] = label
		analysis.HardBlocked = analysis.HardBlocked || hardBlockBehaviors[id]
		analysis.DenyTier = analysis.DenyTier || denyTierBehaviors[id]
	}
	return analysis, nil
}
