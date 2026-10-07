package ask

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestUpstreamParity(t *testing.T) {
	var fixture struct {
		Validations []struct {
			Name       string
			Params     map[string]any
			Normalized Params
			Validation struct {
				OK             bool
				Error, Message string
			}
		}
		Envelopes []struct {
			Name     string
			Params   Params
			Result   Result
			Response struct {
				Content []struct{ Text string }
				Details Result
			}
		}
	}
	data, err := os.ReadFile("../../testfixtures/ask-questionnaire.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tt := range fixture.Validations {
		t.Run(tt.Name, func(t *testing.T) {
			p, err := Parse(tt.Params)
			if tt.Validation.OK {
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(p, tt.Normalized) {
					t.Fatalf("normalized %#v want %#v", p, tt.Normalized)
				}
				return
			}
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Code != tt.Validation.Error || "Error: "+validation.Message != tt.Validation.Message {
				t.Fatalf("validation %v want %#v", err, tt.Validation)
			}
		})
	}
	for _, tt := range fixture.Envelopes {
		t.Run(tt.Name, func(t *testing.T) {
			text, details := Envelope(tt.Result, tt.Params)
			if text != tt.Response.Content[0].Text || !reflect.DeepEqual(details, tt.Response.Details) {
				t.Fatalf("response %q %#v want %#v", text, details, tt.Response)
			}
		})
	}
}
func TestUTF16Boundaries(t *testing.T) {
	q := sample(false)
	q.Header = "😀😀😀😀😀😀😀😀"
	raw := func() map[string]any {
		data, _ := json.Marshal(Params{Questions: []Question{q}})
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		return m
	}
	if _, err := Parse(raw()); err != nil {
		t.Fatal(err)
	}
	q.Header += "a"
	if _, err := Parse(raw()); err == nil {
		t.Fatal("accepted 17 UTF-16 code units")
	}
}
