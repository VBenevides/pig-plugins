package cursorbridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestSandboxStreamRejectionKeepsSecurityAndDoesNotReplay(t *testing.T) {
	m := &mockBridge{t: t}
	m.onSend = func(_ *mockBridge, w http.ResponseWriter, _ *http.Request, _ string) {
		frame(w, 2, map[string]any{"error": map[string]any{"code": "invalid_argument", "message": sandboxUnavailable}})
	}
	server := m.server()
	defer server.Close()
	p := processFor(server)
	defer p.Client.Close()
	out, err := p.RunTurn(context.Background(), "private-key", Selection{ID: "model"}, readTool, "private-prompt", func(Event) error { return nil })
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Reason != "sandbox_unavailable" || !strings.Contains(err.Error(), "send Cursor turn") || !strings.Contains(err.Error(), "keeps sandboxing enabled") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "Disable local.sandboxOptions") || strings.Contains(err.Error(), "private-key") || strings.Contains(err.Error(), "private-prompt") {
		t.Fatal("unsafe remote text in error")
	}
	if m.call("Send") != 1 || m.call("CreateAgent") != 1 || m.call("CloseAgent") != 1 || out.CleanupError != nil {
		t.Fatalf("unexpected lifecycle: sends=%d creates=%d closes=%d cleanup=%v", m.call("Send"), m.call("CreateAgent"), m.call("CloseAgent"), out.CleanupError)
	}
}

func TestRejectionsExposeOnlyFixedDiagnostics(t *testing.T) {
	for _, tc := range []struct{ code, reason string }{
		{"SDK_ERROR_CODE_INVALID_MODEL", "invalid_model"},
		{"SDK_ERROR_CODE_VALIDATION_ERROR", "validation_error"},
		{"SDK_ERROR_CODE_PLAN_REQUIRED", "plan_required"},
		{"private-secret", ""},
	} {
		t.Run(tc.code, func(t *testing.T) {
			data, _ := json.Marshal(map[string]any{"code": "invalid_argument", "message": "private-secret", "details": []any{map[string]any{"type": "sdk.v1.SdkErrorDetails", "debug": map[string]any{"sdkErrorCode": tc.code, "message": "private-secret"}}}})
			err := decodeError(data, 200)
			var rpc *RPCError
			if !errors.As(err, &rpc) || rpc.Reason != tc.reason || strings.Contains(err.Error(), "private-secret") || strings.Contains(err.Error(), "HTTP 200") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
	// Near matches must never copy arbitrary remote prose, including appended tokens.
	err := decodeError([]byte(`{"code":"invalid_argument","message":`+string(mustJSON(t, sandboxUnavailable+" private-secret"))+`}`), 200)
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Reason != "" || strings.Contains(err.Error(), "private-secret") {
		t.Fatalf("near-match: %v", err)
	}
}

func mustJSON(t *testing.T, value string) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
