package toolsettings

import (
	"fmt"
	"os"
	"strings"
)

// ProfileEnv carries a launch choice to extension cells and reloads.
const ProfileEnv = "PIG_PLUGINS_LAUNCH_PROFILE"

// ParseProfile removes distribution-owned flags, leaving all other arguments unchanged.
// A literal -- ends flag parsing. Duplicate profiles are rejected.
func ParseProfile(args []string) ([]string, string, error) {
	out := make([]string, 0, len(args))
	profile := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			out = append(out, args[i:]...)
			break
		}
		if arg != "--profile" && !strings.HasPrefix(arg, "--profile=") {
			out = append(out, arg)
			continue
		}
		if profile != "" {
			return nil, "", fmt.Errorf("--profile can be specified only once")
		}
		value := strings.TrimPrefix(arg, "--profile=")
		if arg == "--profile" {
			i++
			if i >= len(args) {
				return nil, "", fmt.Errorf("--profile requires default, minimal, or default-benchmark")
			}
			value = args[i]
		}
		value = strings.ToLower(value)
		if value != DefaultPreset && value != MinimalPreset && value != BenchmarkPreset {
			return nil, "", fmt.Errorf("unknown profile %q; use default, minimal, or default-benchmark", value)
		}
		profile = value
	}
	return out, profile, nil
}

// LoadEffective applies a process-only profile without changing the saved configuration.
func LoadEffective(path string) (Config, error) {
	c, err := Load(path)
	if err != nil {
		return c, err
	}
	switch profile := strings.ToLower(os.Getenv(ProfileEnv)); profile {
	case "":
		return c, nil
	case DefaultPreset, BenchmarkPreset:
		c.Reset()
		if profile == BenchmarkPreset {
			c.DisabledTools["ask_user_question"] = true
		}
	case MinimalPreset:
		c.NativeFiles = true
		c.DisabledTools = map[string]bool{}
		c.EnabledTools = map[string]bool{"read": true, "bash": true, "edit": true, "write": true}
		c.ToolAllowlist = []string{"read", "bash", "edit", "write"}
		c.SkillAllowlist = []string{}
	default:
		return c, fmt.Errorf("invalid launch profile %q", profile)
	}
	return c, nil
}
