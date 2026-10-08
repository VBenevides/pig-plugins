package guard

import (
	"os"
	"strings"
)

// commandAffectedItems deliberately handles only simple commands with explicit targets.
// Shell expansion, compound commands and unsupported options fall back to an unknown scope.
// This is presentation only: do not use these descriptions to authorize operations.
func commandAffectedItems(command string, analysis Analysis, items []string) []string {
	if strings.ContainsAny(command, "$`\\;&|<>\n\r(){}*?[]#") {
		return nil
	}
	tokens := roughTokenize(command)
	for i, token := range tokens {
		if strings.ContainsAny(token, "\"'") {
			if len(token) < 2 || (token[0] != '\'' && token[0] != '"') || token[len(token)-1] != token[0] || strings.ContainsAny(token[1:len(token)-1], "\"'") {
				return nil
			}
			tokens[i] = token[1 : len(token)-1]
		}
	}
	if len(tokens) < 2 {
		return nil
	}
	if tokens[0] == "rm" && len(analysis.DeleteTargets) > 0 {
		// The policy parser splits quoted rm targets on whitespace; do not present those as resolved paths.
		if strings.ContainsAny(command, "\"'") {
			return nil
		}
		var lines []string
		for _, path := range items {
			kind, effect := "path", "delete (file or folder; type unknown)"
			if info, err := os.Lstat(path); err == nil {
				effect = "delete"
				switch {
				case info.Mode()&os.ModeSymlink != 0:
					kind = "symlink"
				case info.IsDir():
					kind = "folder"
				default:
					kind = "file"
				}
			}
			lines = append(lines, "- "+kind+": "+path+" - "+effect)
		}
		return lines
	}
	if tokens[0] != "git" {
		return nil
	}
	switch tokens[1] {
	case "push":
		return pushAffectedItems(tokens[2:])
	case "branch", "tag":
		kind, effect := tokens[1], "delete local "+tokens[1]
		var names []string
		deleted, endOptions := false, false
		for _, arg := range tokens[2:] {
			switch {
			case !endOptions && arg == "--":
				endOptions = true
			case !endOptions && (arg == "-D" && kind == "branch" || arg == "-d" || arg == "--delete"):
				deleted = true
			case !endOptions && strings.HasPrefix(arg, "-"):
				return nil
			default:
				names = append(names, "- "+kind+": "+arg+" - "+effect)
			}
		}
		if deleted {
			return names
		}
	}
	return nil
}

func pushAffectedItems(args []string) []string {
	var operands []string
	force, deleteRefs, endOptions := false, false, false
	for _, arg := range args {
		switch {
		case !endOptions && arg == "--":
			endOptions = true
		case !endOptions && (arg == "--force" || arg == "-f" || arg == "--force-with-lease" || strings.HasPrefix(arg, "--force-with-lease=") || arg == "--force-if-includes"):
			force = true
		case !endOptions && (arg == "--delete" || arg == "-d"):
			deleteRefs = true
		case !endOptions && (arg == "--set-upstream" || arg == "-u"):
		case !endOptions && strings.HasPrefix(arg, "-"):
			return nil
		default:
			operands = append(operands, arg)
		}
	}
	if len(operands) < 2 {
		return nil // No explicit destination refs: never guess the current branch or configured push scope.
	}
	if operands[1] == "tag" {
		if len(operands) != 3 {
			return nil
		}
		operands = []string{operands[0], "refs/tags/" + operands[2]}
	}
	var lines []string
	for _, ref := range operands[1:] {
		effect := "push changes"
		refForce := strings.HasPrefix(ref, "+")
		ref = strings.TrimPrefix(ref, "+")
		source, target, mapped := strings.Cut(ref, ":")
		if mapped {
			if strings.Contains(target, ":") || target == "" {
				return nil
			}
			ref = target
		}
		if deleteRefs || mapped && source == "" {
			effect = "delete remote reference"
		} else if force || refForce {
			effect += " (overwrite remote history)"
		}
		kind := "branch"
		switch {
		case strings.HasPrefix(ref, "refs/tags/"):
			kind, ref = "tag", strings.TrimPrefix(ref, "refs/tags/")
		case strings.HasPrefix(ref, "refs/heads/"):
			ref = strings.TrimPrefix(ref, "refs/heads/")
		case strings.HasPrefix(ref, "refs/"):
			kind = "ref"
		case ref == "HEAD" || ref == "" || strings.ContainsAny(ref, "~^"):
			return nil
		}
		lines = append(lines, "- "+kind+": "+ref+" (remote: "+operands[0]+") - "+effect)
	}
	return lines
}
