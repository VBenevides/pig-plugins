// Command prompt-audit inventories saved JSONL traces without running models or benchmarks.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"unicode/utf8"
)

type traceRecord struct {
	Type    string `json:"type"`
	Message struct {
		Role         string            `json:"role"`
		Sections     map[string]string `json:"sections"`
		ToolsAdded   []json.RawMessage `json:"toolsAdded"`
		ToolsRemoved []string          `json:"toolsRemoved"`
	} `json:"message"`
}
type systemRecord struct {
	Line           int      `json:"line"`
	Characters     int      `json:"section_characters"`
	SectionsSHA256 string   `json:"canonical_sections_sha256"`
	ActiveTools    []string `json:"active_tools"`
	SchemasSHA256  string   `json:"canonical_schemas_sha256"`
}
type traceInventory struct {
	Path    string         `json:"path"`
	SHA256  string         `json:"sha256"`
	Systems []systemRecord `json:"systems"`
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func inventory(path, root string) (traceInventory, error) {
	f, err := os.Open(path)
	if err != nil {
		return traceInventory{}, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	scan := bufio.NewScanner(io.TeeReader(f, h))
	scan.Buffer(make([]byte, 65536), 16<<20)
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return traceInventory{}, err
	}
	result := traceInventory{Path: filepath.ToSlash(relative), Systems: []systemRecord{}}
	active := map[string]json.RawMessage{}
	line := 0
	for scan.Scan() {
		line++
		var record traceRecord
		if err := json.Unmarshal(scan.Bytes(), &record); err != nil {
			return result, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if record.Type != "message_end" || record.Message.Role != "system" {
			continue
		}
		for _, name := range record.Message.ToolsRemoved {
			delete(active, name)
		}
		for _, raw := range record.Message.ToolsAdded {
			var tool struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(raw, &tool); err != nil {
				return result, fmt.Errorf("%s:%d tool: %w", path, line, err)
			}
			if tool.Name == "" {
				return result, fmt.Errorf("%s:%d: tool has no name", path, line)
			}
			active[tool.Name] = raw
		}
		sections, err := json.Marshal(record.Message.Sections)
		if err != nil {
			return result, err
		}
		schemas, err := json.Marshal(active)
		if err != nil {
			return result, err
		}
		entry := systemRecord{Line: line, SectionsSHA256: digest(sections), SchemasSHA256: digest(schemas), ActiveTools: []string{}}
		for _, text := range record.Message.Sections {
			entry.Characters += utf8.RuneCountInString(text)
		}
		for name := range active {
			entry.ActiveTools = append(entry.ActiveTools, name)
		}
		sort.Strings(entry.ActiveTools)
		result.Systems = append(result.Systems, entry)
	}
	if err := scan.Err(); err != nil {
		return result, fmt.Errorf("%s:%d: %w", path, line+1, err)
	}
	result.SHA256 = hex.EncodeToString(h.Sum(nil))
	return result, nil
}

func run(root string, out io.Writer) error {
	paths := []string{}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == "stdout.jsonl" {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no stdout.jsonl traces under %s", root)
	}
	sort.Strings(paths)
	results := make([]traceInventory, 0, len(paths))
	for _, path := range paths {
		result, err := inventory(path, root)
		if err != nil {
			return err
		}
		results = append(results, result)
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(results)
}

func main() {
	root := flag.String("root", "", "existing benchmark result directory (read-only)")
	flag.Parse()
	if *root == "" {
		fmt.Fprintln(os.Stderr, "prompt-audit: -root is required")
		os.Exit(2)
	}
	if err := run(*root, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "prompt-audit:", err)
		os.Exit(1)
	}
}
