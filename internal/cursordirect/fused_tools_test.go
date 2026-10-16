package cursordirect

import (
	"bytes"
	"net/http"
	"testing"
)

// The provider's initial request frame has already been validated by the fixture.
// Request missing root blobs on continuation to prove the actual native result
// reached the provider, not just the displayed transcript.
func fusedCursorToolResponse(t *testing.T, w http.ResponseWriter, r *http.Request, request int32, initial []byte) {
	t.Helper()
	w.Header().Set("Content-Type", "application/connect+proto")
	w.WriteHeader(200)
	flush := func(data []byte) {
		if _, err := w.Write(data); err != nil {
			t.Error(err)
		}
		w.(http.Flusher).Flush()
	}
	if request == 2 {
		run := mustParse(t, mustParse(t, initial).data(1))
		state := mustParse(t, run.data(1))
		if !mustParse(t, run.data(2)).has(2) {
			t.Error("native tool continuation lacks resume action")
		}
		found := false
		idx := uint64(0)
		for _, field := range state {
			if field.number != 1 {
				continue
			}
			idx++
			flush(frame(bytesField(4, join(integer(1, idx), bytesField(2, bytesField(1, field.data))))))
			_, reply, err := readFrame(r.Body)
			if err != nil {
				t.Error(err)
				return
			}
			kv := mustParse(t, mustParse(t, reply).data(3))
			data := mustParse(t, kv.data(2)).data(1)
			if bytes.Contains(data, []byte("approved fixture content")) {
				found = true
			}
		}
		if !found {
			t.Error("native tool result absent from provider continuation")
		}
		flush(frame(update(1, str(1, "fixture final answer"))))
		flush(frame(update(14, nil)))
		flush(terminal("{}"))
		return
	}
	flush(frame(bytesField(2, join(integer(1, 1), bytesField(10, nil)))))
	if _, _, err := readFrame(r.Body); err != nil {
		t.Error(err)
		return
	}
	args, err := mcpArgs("fused-call-1", "read", map[string]any{"path": "file.txt"})
	if err != nil {
		t.Error(err)
		return
	}
	flush(frame(bytesField(2, join(integer(1, 2), bytesField(11, args)))))
	_, reply, err := readFrame(r.Body)
	if err != nil {
		t.Error(err)
		return
	}
	if !bytes.Contains(reply, []byte(handoff)) {
		t.Error("missing native tool handoff")
	}
	flush(frame(update(14, nil)))
	flush(terminal("{}"))
}
