package compactlimit

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestMeasureRequestIncludesTheWholePayloadAndNoOldSavings(t *testing.T) {
	payload := map[string]any{
		"model": "model-a", "max_tokens": 2000,
		"messages": []any{map[string]any{"role": "system", "content": "constraints"},
			map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"arguments": strings.Repeat("write", 1000)}}},
			map[string]any{"role": "tool", "content": "é 😀 tool output [recovery: short]"}},
		"tools": []any{map[string]any{"name": "read", "parameters": map[string]any{"type": "object"}}},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	got, err := MeasureRequest(payload, "session/branch/provider/model-a", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if got.SerializedBytes != len(data) || got.Tokens != len(data) || got.OutputReserve != 2000 || got.UnboundedMedia {
		t.Fatalf("wrong complete payload measurement: %+v", got)
	}
	repeated, err := MeasureRequest(json.RawMessage(data), "session/branch/provider/model-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Fingerprint != got.Fingerprint || repeated.Tokens != got.Tokens {
		t.Fatal("measurement must not subtract reservation or previous reductions")
	}
	for _, identity := range []string{"session/other/provider/model-a", "session/branch/provider/model-b", "other/branch/provider/model-a"} {
		other, err := MeasureRequest(payload, identity, 2000)
		if err != nil {
			t.Fatal(err)
		}
		if got.Fingerprint == other.Fingerprint {
			t.Fatal("identity change reused a request fingerprint")
		}
	}
	payload["tools"] = []any{map[string]any{"description": strings.Repeat("schema", 100)}}
	changed, err := MeasureRequest(payload, "session/branch/provider/model-a", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Fingerprint == got.Fingerprint || changed.Tokens == got.Tokens {
		t.Fatal("schema change was not measured")
	}
}

func TestMeasureRequestMarksMediaAndOpaqueStateUnbounded(t *testing.T) {
	for _, block := range []any{
		map[string]any{"type": "image_url", "image_url": "https://example.invalid/image"},
		map[string]any{"type": "input_audio", "data": "abc"},
		map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": "abc"}},
		map[string]any{"type": "compaction", "encrypted_content": "opaque"},
	} {
		got, err := MeasureRequest(map[string]any{"messages": []any{block}}, "model", 0)
		if err != nil || !got.UnboundedMedia {
			t.Fatalf("media must not claim an upper token bound: %+v %v", got, err)
		}
	}
}

func TestMeasureRequestRejectsInvalidPayloads(t *testing.T) {
	for _, payload := range []any{nil, []any{}, "body", map[string]any{"bad": math.NaN()}, json.RawMessage(`{`)} {
		if _, err := MeasureRequest(payload, "model", 0); err == nil {
			t.Fatalf("accepted invalid payload %T", payload)
		}
	}
	if _, err := MeasureRequest(map[string]any{}, "model", -1); err == nil {
		t.Fatal("accepted negative reserve")
	}
}
