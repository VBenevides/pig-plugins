package compactlimit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// RequestObserved runs after all before_provider_request transforms in the patched host.
const RequestObserved = "provider_request_observed"

// RequestMeasurement describes a complete adapter payload, not billed token usage.
// SerializedBytes includes instructions, schemas, arguments, results, and JSON framing.
// Tokens is a conservative text estimate of one token per serialized UTF-8 byte.
// Media and provider-owned opaque state prevent a claim that this estimate bounds tokens.
type RequestMeasurement struct {
	Fingerprint     string
	SerializedBytes int
	Tokens          int
	OutputReserve   int
	UnboundedMedia  bool
}

// MeasureRequest never subtracts prior savings or prefix-cache hits from a new request.
// The fingerprint identifies canonical JSON and model identity, not cache-prefix bytes.
func MeasureRequest(payload any, identity string, outputReserve int) (RequestMeasurement, error) {
	if outputReserve < 0 {
		return RequestMeasurement{}, fmt.Errorf("measure request: output reserve must not be negative")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return RequestMeasurement{}, fmt.Errorf("measure request payload: %w", err)
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return RequestMeasurement{}, fmt.Errorf("decode request payload: %w", err)
	}
	if _, ok := decoded.(map[string]any); !ok {
		return RequestMeasurement{}, fmt.Errorf("measure request: payload must be an object")
	}
	// Normalize raw JSON and typed payloads before hashing.
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return RequestMeasurement{}, fmt.Errorf("normalize request payload: %w", err)
	}
	hash := sha256.New()
	hash.Write([]byte(identity))
	hash.Write([]byte{0})
	hash.Write(canonical)
	return RequestMeasurement{
		Fingerprint: hex.EncodeToString(hash.Sum(nil)), SerializedBytes: len(data),
		Tokens: len(data), OutputReserve: outputReserve, UnboundedMedia: unboundedContent(decoded),
	}, nil
}

func unboundedContent(value any) bool {
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			if unboundedContent(item) {
				return true
			}
		}
	case map[string]any:
		if kind, _ := value["type"].(string); kind == "image" || kind == "image_url" || kind == "input_image" || kind == "input_audio" || kind == "audio" || kind == "video" || kind == "compaction" {
			return true
		}
		for key, item := range value {
			if key == "encrypted_content" || key == "inlineData" || key == "fileData" || key == "document" || key == "image" || key == "audio" || key == "video" {
				return true
			}
			if unboundedContent(item) {
				return true
			}
		}
	}
	return false
}
