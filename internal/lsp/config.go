package lsp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
	"github.com/VBenevides/pig-plugins/internal/fsutil"
)

// Server retains the pi-lsp@0.1.7 configuration fields.
type Server struct {
	ID                    string `json:"id"`
	fallbackRoot          bool
	Enabled               *bool             `json:"enabled,omitempty"`
	Include               []string          `json:"include,omitempty"`
	Exclude               []string          `json:"exclude,omitempty"`
	RootMarkers           []string          `json:"rootMarkers,omitempty"`
	Bin                   string            `json:"bin"`
	Args                  []string          `json:"args,omitempty"`
	Cwd                   string            `json:"cwd,omitempty"`
	Env                   map[string]string `json:"env,omitempty"`
	Config                string            `json:"config,omitempty"`
	LanguageID            map[string]string `json:"languageIdByExtension,omitempty"`
	MaxFileSize           int64             `json:"maxFileSizeBytes,omitempty"`
	StartupMS             int               `json:"startupTimeoutMs,omitempty"`
	DiagnosticsMS         int               `json:"diagnosticsWaitMs,omitempty"`
	InitializationOptions any               `json:"initializationOptions,omitempty"`
	Settings              any               `json:"settings,omitempty"`
}

func (s Server) startTimeout() time.Duration {
	if s.StartupMS > 0 {
		return time.Duration(min(s.StartupMS, 120000)) * time.Millisecond
	}
	return 45 * time.Second
}
func (s Server) diagTimeout() time.Duration {
	if s.DiagnosticsMS > 0 {
		return time.Duration(min(s.DiagnosticsMS, 30000)) * time.Millisecond
	}
	return 1500 * time.Millisecond
}
func (s Server) fileLimit() int64 {
	if s.MaxFileSize > 0 {
		return min(s.MaxFileSize, 4<<20)
	}
	return 2 << 20
}

// Trust prompts before accepting project-local executable configuration. The hash always covers the exact bytes.
type Trust func(path, hash string, binaries []string) (trusted, persist bool, err error)

func findUp(dir, name string) string {
	for {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
func parseConfig(data []byte) ([]Server, error) {
	var doc struct {
		Servers []Server `json:"servers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	var out []Server
	for _, s := range doc.Servers {
		if strings.TrimSpace(s.ID) == "" {
			continue
		}
		if s.Enabled != nil && !*s.Enabled {
			out = append(out, s)
			continue
		}
		if strings.TrimSpace(s.Bin) == "" {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// Load combines the operator config with a hash-trusted project config. Config files do not execute anything here.
func Load(cwd string, getenv func(string) string, trust Trust) ([]Server, error) {
	global := agentdir.File(getenv, "lsp.json")
	if dir := getenv("PI_AGENT_DIR"); dir != "" {
		global = filepath.Join(dir, "lsp.json")
	}
	var result []Server
	data, err := os.ReadFile(global)
	if err == nil {
		result, err = parseConfig(data)
		if err != nil {
			return nil, fmt.Errorf("global LSP config: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else {
		result, err = defaults(getenv)
		if err != nil {
			return nil, err
		}
	}
	project := findUp(cwd, filepath.Join(".pi", "lsp.json"))
	if project == "" {
		return result, nil
	}
	data, err = os.ReadFile(project)
	if err != nil {
		return nil, err
	}
	items, err := parseConfig(data)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	storeFile := filepath.Join(filepath.Dir(global), "trust", "lsp.json")
	var store struct {
		Version int      `json:"version"`
		Hashes  []string `json:"trustedHashes"`
	}
	_, err = fsutil.ReadJSONFile(storeFile, &store)
	if err != nil {
		return nil, fmt.Errorf("LSP trust store: %w", err)
	}
	trusted := slices.Contains(store.Hashes, hash)
	if !trusted && trust != nil {
		var bins []string
		for _, s := range items {
			if s.Bin != "" {
				bins = append(bins, s.Bin)
			}
		}
		var persist bool
		trusted, persist, err = trust(project, hash, bins)
		if err != nil {
			return nil, err
		}
		if trusted && persist {
			store.Version = 1
			store.Hashes = append(store.Hashes, hash)
			if err = fsutil.WriteJSONFileAtomic(storeFile, store, 0600); err != nil {
				return nil, err
			}
		}
	}
	if !trusted {
		return nil, fmt.Errorf("project-local LSP config rejected: %s", project)
	}
	for _, s := range items {
		idx := slices.IndexFunc(result, func(other Server) bool { return other.ID == s.ID })
		if s.Enabled != nil && !*s.Enabled {
			if idx >= 0 {
				result = slices.Delete(result, idx, idx+1)
			}
			continue
		}
		if idx >= 0 {
			result[idx] = s
		} else {
			result = append(result, s)
		}
	}
	return result, nil
}
func matches(pattern, path string) bool {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	b.WriteString("$")
	ok, _ := regexp.MatchString(b.String(), filepath.ToSlash(path))
	return ok
}
func included(s Server, rel string) bool {
	for _, p := range s.Exclude {
		if matches(p, rel) {
			return false
		}
	}
	if len(s.Include) == 0 {
		return true
	}
	for _, p := range s.Include {
		if matches(p, rel) {
			return true
		}
	}
	return false
}
func rootFor(file, cwd string, markers []string) string {
	if len(markers) == 0 {
		return cwd
	}
	dir := filepath.Dir(file)
	for {
		for _, m := range markers {
			if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
func absolute(path, cwd, home string) string {
	if strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	return filepath.Clean(path)
}
func resolve(s Server, root, file, cwd string, getenv func(string) string) Server {
	rel, _ := filepath.Rel(root, file)
	reldir, _ := filepath.Rel(root, filepath.Dir(file))
	values := map[string]string{"workspace": cwd, "root": root, "file": file, "relFile": filepath.ToSlash(rel), "dir": filepath.Dir(file), "relDir": filepath.ToSlash(reldir), "config": "", "configDir": ""}
	apply := func(text string) string {
		for key, value := range values {
			text = strings.ReplaceAll(text, "{"+key+"}", value)
		}
		return text
	}
	if s.Config != "" {
		s.Config = absolute(apply(s.Config), root, getenv("HOME"))
		values["config"] = s.Config
		values["configDir"] = filepath.Dir(s.Config)
	}
	s.Bin = apply(s.Bin)
	if strings.ContainsAny(s.Bin, "/\\") {
		s.Bin = absolute(s.Bin, root, getenv("HOME"))
	} else {
		dirs := filepath.SplitList(getenv("PATH"))
		dirs = append(dirs, filepath.SplitList(getenv("ZED_PI_HARNESS_LSP_SEARCH_PATH"))...)
		dirs = append(dirs, filepath.Join(getenv("HOME"), ".local/share/nvim/mason/bin"))
		for _, dir := range dirs {
			candidate := filepath.Join(dir, s.Bin)
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
				s.Bin = candidate
				break
			}
		}
	}
	s.Args = slices.Clone(s.Args)
	for i, a := range s.Args {
		s.Args[i] = apply(a)
	}
	s.Cwd = absolute(apply(s.Cwd), root, getenv("HOME"))
	env := map[string]string{}
	for k, v := range s.Env {
		env[k] = apply(v)
	}
	s.Env = env
	return s
}
func defaults(getenv func(string) string) ([]Server, error) {
	groups := []struct {
		id         string
		bins       [][]string
		extensions []string
		markers    []string
	}{
		{"typescript", [][]string{{"typescript-language-server", "--stdio"}}, []string{"ts", "mts", "cts", "tsx", "js", "mjs", "cjs", "jsx"}, []string{"tsconfig.json", "jsconfig.json", "package.json", ".git"}},
		{"python", [][]string{{"pyright-langserver", "--stdio"}, {"basedpyright-langserver", "--stdio"}, {"pylsp"}, {"jedi-language-server"}}, []string{"py", "pyi"}, []string{"pyproject.toml", "setup.py", "requirements.txt", ".git"}},
		{"go", [][]string{{"gopls"}}, []string{"go"}, []string{"go.work", "go.mod", ".git"}},
		{"rust", [][]string{{"rust-analyzer"}}, []string{"rs"}, []string{"Cargo.toml", ".git"}},
		{"bash", [][]string{{"bash-language-server", "start"}}, []string{"sh", "bash"}, []string{".git"}},
		{"lua", [][]string{{"lua-language-server"}}, []string{"lua"}, []string{".luarc.json", ".git"}},
		{"markdown", [][]string{{"marksman", "server"}}, []string{"md", "markdown"}, []string{".marksman.toml", ".git"}},
		{"json", [][]string{{"vscode-json-language-server", "--stdio"}}, []string{"json", "jsonc"}, []string{".git"}},
		{"c", [][]string{{"clangd"}}, []string{"c", "h", "cc", "cpp", "cxx", "hpp"}, []string{"compile_commands.json", ".clangd", ".git"}},
	}
	var out []Server
	for _, g := range groups {
		argv := g.bins[0]
		if override := getenv("ZED_PI_HARNESS_LSP_" + strings.ToUpper(g.id)); override != "" {
			if strings.HasPrefix(strings.TrimSpace(override), "[") {
				if json.Unmarshal([]byte(override), &argv) != nil || len(argv) == 0 {
					return nil, fmt.Errorf("invalid ZED_PI_HARNESS_LSP_%s command array", strings.ToUpper(g.id))
				}
			} else {
				argv = strings.Fields(override)
			}
			if len(argv) == 0 || slices.Contains(argv, "") {
				return nil, fmt.Errorf("invalid ZED_PI_HARNESS_LSP_%s command", strings.ToUpper(g.id))
			}
		} else {
			for _, candidate := range g.bins {
				s := resolve(Server{Bin: candidate[0]}, getenv("HOME"), "", getenv("HOME"), getenv)
				if filepath.IsAbs(s.Bin) {
					argv = candidate
					break
				}
			}
		}
		s := Server{ID: g.id, Bin: argv[0], Args: argv[1:], RootMarkers: g.markers, LanguageID: map[string]string{}, fallbackRoot: true}
		for _, ext := range g.extensions {
			s.Include = append(s.Include, "**/*."+ext)
			language := ext
			switch g.id {
			case "python", "go", "rust", "lua", "markdown", "json":
				language = g.id
			case "bash":
				language = "shellscript"
			case "typescript":
				language = "typescript"
				if strings.HasPrefix(ext, "j") || ext == "mjs" || ext == "cjs" {
					language = "javascript"
				}
				if strings.HasSuffix(ext, "x") {
					language += "react"
				}
			case "c":
				language = "c"
				if ext != "c" && ext != "h" {
					language = "cpp"
				}
			}
			s.LanguageID["."+ext] = language
		}
		out = append(out, s)
	}
	return out, nil
}
