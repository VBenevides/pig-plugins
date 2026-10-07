package lsp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// Manager lazily starts and owns one client per configured server/root. Its pool and restart attempts are bounded.
type Manager struct {
	Getenv   func(string) string
	mu       sync.Mutex
	clients  map[string]*Client
	failures map[string]int
	closed   bool
}

func NewManager(getenv func(string) string) *Manager {
	return &Manager{Getenv: getenv, clients: map[string]*Client{}, failures: map[string]int{}}
}

type Opened struct {
	Client    *Client
	URI, Text string
	Mark      uint64
	Changed   bool
	Err       error
}
type match struct {
	server Server
	root   string
}

func (m *Manager) matches(path, cwd string, trust Trust) (string, []match, error) {
	file := absolute(path, cwd, m.Getenv("HOME"))
	servers, err := Load(cwd, m.Getenv, trust)
	if err != nil {
		return "", nil, err
	}
	var matches []match
	for _, s := range servers {
		if s.Enabled != nil && !*s.Enabled {
			continue
		}
		root := rootFor(file, cwd, s.RootMarkers)
		if root == "" && s.fallbackRoot {
			root = cwd
		}
		if root == "" {
			continue
		}
		rel, _ := filepath.Rel(root, file)
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || !included(s, rel) {
			continue
		}
		matches = append(matches, match{s, root})
	}
	if len(matches) == 0 {
		return file, nil, fmt.Errorf("no matching LSP server for %s", path)
	}
	return file, matches, nil
}
func (m *Manager) open(ctx context.Context, file, cwd string, selected match) Opened {
	server, root := selected.server, selected.root
	key := server.ID + "\x00" + root
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Opened{Err: errors.New("LSP session closed")}
	}
	client := m.clients[key]
	if client == nil || !client.Alive() {
		if m.failures[key] >= 2 {
			return Opened{Err: fmt.Errorf("%s failed twice; restart the session", server.ID)}
		}
		if len(m.clients) >= 32 && client == nil {
			return Opened{Err: errors.New("LSP server pool limit (32)")}
		}
		if client != nil {
			m.failures[key]++
			client.Close()
		}
		if m.failures[key] >= 2 {
			return Opened{Err: fmt.Errorf("%s failed twice; restart the session", server.ID)}
		}
		var err error
		client, err = Start(ctx, resolve(server, root, file, cwd, m.Getenv), root)
		if err != nil {
			m.failures[key]++
			return Opened{Err: err}
		}
		m.clients[key] = client
	}
	language := server.LanguageID[filepath.Ext(file)]
	if language == "" {
		language = strings.TrimPrefix(filepath.Ext(file), ".")
	}
	uri, text, mark, changed, err := client.Sync(ctx, file, language)
	return Opened{Client: client, URI: uri, Text: text, Mark: mark, Changed: changed, Err: err}
}

// Open synchronizes a document with the first matching server, as the upstream navigation tools do.
func (m *Manager) Open(ctx context.Context, path, cwd string, trust Trust) (*Client, string, string, uint64, bool, error) {
	file, matches, err := m.matches(path, cwd, trust)
	if err != nil {
		return nil, "", "", 0, false, err
	}
	o := m.open(ctx, file, cwd, matches[0])
	return o.Client, o.URI, o.Text, o.Mark, o.Changed, o.Err
}

// OpenAll keeps failures separate when multiple configured servers match a post-edit document.
func (m *Manager) OpenAll(ctx context.Context, path, cwd string, trust Trust) ([]Opened, error) {
	file, matches, err := m.matches(path, cwd, trust)
	if err != nil {
		return nil, err
	}
	var out []Opened
	for _, s := range matches {
		out = append(out, m.open(ctx, file, cwd, s))
	}
	return out, nil
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	clients := m.clients
	m.clients = map[string]*Client{}
	m.mu.Unlock()
	for _, c := range clients {
		c.Close()
	}
}
