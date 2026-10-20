package cursordirect

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This opt-in acceptance test uses the actual registered Cursor factory in a
// freshly built distribution. HTTPS is redirected by a loopback-only CONNECT
// proxy and validated against an ephemeral CA; TLS verification stays enabled.
func TestFusedCursorGenerationReplayVeto(t *testing.T) {
	binary := os.Getenv("PIG_FUSED_TEST_BINARY")
	if binary == "" {
		t.Skip("set PIG_FUSED_TEST_BINARY to a freshly built patched distribution")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(binary); err != nil {
		t.Fatal(err)
	}
	var fragmented []byte
	for range 2000 {
		fragmented = append(fragmented, frame(update(1, str(1, strings.Repeat("x", 64))))...)
	}
	fragmented = append(fragmented, terminal(`{"error":{"code":"unavailable"}}`)...)
	for _, tc := range []struct {
		name   string
		status int
		data   []byte
	}{
		{"HTTP 503", 503, nil},
		{"Connect terminal", 200, terminal(`{"error":{"code":"unavailable"}}`)},
		{"partial output then failure", 200, join(frame(update(1, str(1, "partial fixture output"))), terminal(`{"error":{"code":"unavailable"}}`))},
		{"fragmented output then failure", 200, fragmented},
		{"successful tool round trip", 200, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			agent := filepath.Join(home, "agent")
			if err := os.Mkdir(agent, 0700); err != nil {
				t.Fatal(err)
			}
			var requests, catalogs, unexpected atomic.Int32
			backend := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer synthetic-replay-only" {
					unexpected.Add(1)
					http.Error(w, "fixture auth mismatch", 401)
					return
				}
				switch r.URL.Path {
				case usablePath:
					catalogs.Add(1)
					_, _ = w.Write(bytesField(1, join(str(1, "test-model"), str(4, "Fixture model"), bytesField(2, nil))))
				case availablePath:
					_, _ = w.Write(bytesField(2, join(str(1, "test-model"), str(36, "test-model"), integer(15, 65536))))
				case runPath:
					if requests.Add(1) > 4 {
						http.Error(w, "fixture request budget exceeded", 400)
						return
					}
					// A generation must actually arrive over HTTP/2, not stop at auth or setup.
					if r.ProtoMajor != 2 {
						unexpected.Add(1)
						http.Error(w, "HTTP/2 required", 400)
						return
					}
					_, initial, err := readFrame(io.LimitReader(r.Body, maxFrame+5))
					if err != nil {
						unexpected.Add(1)
						http.Error(w, "invalid request frame", 400)
						return
					}
					if tc.name == "successful tool round trip" {
						fusedCursorToolResponse(t, w, r, requests.Load(), initial)
						return
					}
					if tc.status != 200 {
						w.WriteHeader(tc.status)
						return
					}
					writeFrames(w, tc.data)
				default:
					unexpected.Add(1)
					http.Error(w, "unapproved fixture path", 400)
				}
			}))
			certificate, ca := replayFixtureCertificate(t)
			backend.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
			backend.EnableHTTP2 = true
			backend.StartTLS()
			defer backend.Close()
			proxy := replayFixtureProxy(t, backend.Listener.Addr().String(), &unexpected)
			defer proxy.Close()
			caPath := filepath.Join(home, "ca.pem")
			if err := os.WriteFile(caPath, ca, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, "file.txt"), []byte("approved fixture content"), 0600); err != nil {
				t.Fatal(err)
			}
			writeReplayFixtureJSON(t, filepath.Join(agent, "auth.json"), map[string]any{"cursor": map[string]any{"type": "oauth", "access": "synthetic-replay-only", "refresh": "synthetic-refresh-only", "expires": time.Now().Add(time.Hour).UnixMilli()}})
			writeReplayFixtureJSON(t, filepath.Join(agent, "settings.json"), map[string]any{"quietStartup": true, "compaction": map[string]any{"enabled": false}, "retry": map[string]any{"enabled": true, "maxRetries": 2, "baseDelayMs": 1, "maxAgentDelayMs": 1}})
			slot := map[string]any{"provider": "cursor", "model": "test-model", "thinking": "off"}
			writeReplayFixtureJSON(t, filepath.Join(agent, "auto-model.json"), map[string]any{"primary": slot, "fallback": slot})
			writeReplayFixtureJSON(t, filepath.Join(agent, "check-update.json"), map[string]any{"enabled": false})
			ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "--no-session", "--no-skills", "--no-prompt-templates", "--no-context-files", "--tools", "read", "--mode", "rpc")
			cmd.Dir = home
			cmd.Env = []string{"PATH=" + filepath.Join(home, "empty-path"), "HOME=" + home, "PIG_CODING_AGENT_DIR=" + agent, "HTTPS_PROXY=" + proxy.URL, "HTTP_PROXY=" + proxy.URL, "NO_PROXY=", "SSL_CERT_FILE=" + caPath, "SSL_CERT_DIR=" + home, "PIG_CURSOR_TRANSPORT=h2", "PIG_PLUGINS_HOST_VERSION=fixture", "TERM=dumb"}
			cmd.WaitDelay = 5 * time.Second
			output, err := runReplayFixtureRPC(ctx, cmd)
			if err != nil {
				t.Fatalf("fused RPC fixture: %v (generation=%d catalogs=%d unexpected=%d): %s", err, requests.Load(), catalogs.Load(), unexpected.Load(), output)
			}
			if tc.name == "successful tool round trip" {
				if requests.Load() != 2 || catalogs.Load() < 1 || unexpected.Load() != 0 || !strings.Contains(output, "approved fixture content") || !strings.Contains(output, "fixture final answer") || !strings.Contains(output, `"type":"tool_execution_end"`) {
					t.Fatalf("tool acceptance generation=%d catalogs=%d unexpected=%d: %s", requests.Load(), catalogs.Load(), unexpected.Load(), output)
				}
				return
			}
			if requests.Load() != 1 || catalogs.Load() < 1 || unexpected.Load() != 0 {
				t.Fatalf("generation=%d catalogs=%d unexpected=%d: %s", requests.Load(), catalogs.Load(), unexpected.Load(), output)
			}
			if !strings.Contains(output, `"replayProhibited":true`) || strings.Contains(output, `"type":"auto_retry_start"`) {
				t.Fatalf("veto not retained in native output: %s", output)
			}
			if tc.status == 503 && !strings.Contains(output, "HTTP 503") {
				t.Fatalf("status diagnostic lost: %s", output)
			}
			if tc.name == "partial output then failure" && !strings.Contains(output, "partial fixture output") {
				t.Fatal("partial output lost")
			}
			if tc.name == "fragmented output then failure" && (!strings.Contains(output, strings.Repeat("x", 128000)) || !strings.Contains(output, "remote Connect unavailable")) {
				t.Fatal("fragmented output or original terminal diagnostic lost")
			}
		})
	}
}

// RPC starts without an explicit model so its normal background catalog refresh
// can discover Cursor. Print mode deliberately performs only offline startup
// refresh, so selecting a dynamically discovered model there cannot bootstrap.
func runReplayFixtureRPC(ctx context.Context, cmd *exec.Cmd) (transcript string, resultErr error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	var output, diagnostics replayFixtureOutput
	cmd.Stderr = &diagnostics
	if err = cmd.Start(); err != nil {
		return "", err
	}
	defer func() {
		stdin.Close()
		// This fixture owns the process; after agent_end only background services
		// remain. Always reap it, including protocol failures and deadline expiry.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		transcript = output.String() + diagnostics.String()
		if ctx.Err() != nil {
			resultErr = ctx.Err()
		}
	}()
	decoder := json.NewDecoder(io.LimitReader(stdout, 4<<20))
	encoder := json.NewEncoder(stdin)
	next := func() (map[string]any, error) {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		if _, err := output.Write(raw); err != nil {
			return nil, err
		}
		var message map[string]any
		if err := json.Unmarshal(raw, &message); err != nil {
			return nil, err
		}
		return message, nil
	}
	response := func(command map[string]any) (map[string]any, error) {
		if err := encoder.Encode(command); err != nil {
			return nil, err
		}
		for {
			message, err := next()
			if err != nil {
				return nil, err
			}
			if message["type"] == "response" && message["id"] == command["id"] {
				if message["success"] != true {
					return nil, fmt.Errorf("RPC %v failed: %v", command["type"], message["error"])
				}
				return message, nil
			}
		}
	}
	available := false
	for attempt := 0; attempt < 100 && !available; attempt++ {
		reply, err := response(map[string]any{"id": "available", "type": "get_available_models"})
		if err != nil {
			return "", err
		}
		data, _ := reply["data"].(map[string]any)
		models, _ := data["models"].([]any)
		for _, value := range models {
			model, _ := value.(map[string]any)
			if model["provider"] == "cursor" && model["id"] == "test-model" {
				available = true
			}
		}
		if !available {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	if !available {
		return "", errors.New("Cursor catalog did not become available")
	}
	if _, err := response(map[string]any{"id": "model", "type": "set_model", "provider": "cursor", "modelId": "test-model"}); err != nil {
		return "", err
	}
	if err := encoder.Encode(map[string]any{"id": "prompt", "type": "prompt", "message": "hello"}); err != nil {
		return "", err
	}
	for {
		message, err := next()
		if err != nil {
			return "", err
		}
		if message["type"] == "response" && message["success"] == false {
			return "", fmt.Errorf("prompt failed: %v", message["error"])
		}
		// agent_end can precede a scheduled retry; wait for actual settlement.
		if message["type"] == "agent_end" && message["willRetry"] == false {
			return "", nil
		}
	}
}

// Bound captured child output; a runaway child cannot exhaust the test runner.
type replayFixtureOutput struct{ bytes.Buffer }

func (out *replayFixtureOutput) Write(data []byte) (int, error) {
	if out.Len()+len(data) > 4<<20 {
		return 0, errors.New("fused fixture output budget exceeded")
	}
	return out.Buffer.Write(data)
}

func writeReplayFixtureJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func replayFixtureCertificate(t *testing.T) (tls.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "isolated replay fixture"}, DNSNames: []string{"api2.cursor.sh"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// Never resolve or dial a remote destination. All accepted CONNECTs terminate
// at this test's TLS server. Track hijacked connections for explicit cleanup.
func replayFixtureProxy(t *testing.T, target string, unexpected *atomic.Int32) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	connections := map[net.Conn]struct{}{}
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "CONNECT" && (r.Host == "search.parallel.ai:443" || r.Host == "mcp.exa.ai:443") {
			// The bundled search extension registers these background MCP servers.
			// Deny them locally: this fixture exercises no external discovery.
			http.Error(w, "background MCP disabled by fixture", 403)
			return
		}
		if r.Method != "CONNECT" || r.Host != "api2.cursor.sh:443" || count.Add(1) > 16 {
			unexpected.Add(1)
			t.Logf("fixture rejected unexpected destination %q", r.Host)
			http.Error(w, "fixture proxy forbids this destination", 403)
			return
		}
		dst, err := net.DialTimeout("tcp", target, time.Second)
		if err != nil {
			http.Error(w, "fixture backend unavailable", 502)
			return
		}
		src, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			dst.Close()
			return
		}
		mu.Lock()
		connections[src] = struct{}{}
		connections[dst] = struct{}{}
		mu.Unlock()
		defer func() {
			src.Close()
			dst.Close()
			mu.Lock()
			delete(connections, src)
			delete(connections, dst)
			mu.Unlock()
		}()
		if _, err = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		if err = buffered.Flush(); err != nil {
			return
		}
		done := make(chan struct{})
		go func() { defer close(done); _, _ = io.Copy(dst, buffered); dst.Close() }()
		_, _ = io.Copy(src, dst)
		src.Close()
		dst.Close()
		<-done
	}))
	t.Cleanup(func() {
		server.Close()
		mu.Lock()
		for conn := range connections {
			conn.Close()
		}
		mu.Unlock()
	})
	return server
}
