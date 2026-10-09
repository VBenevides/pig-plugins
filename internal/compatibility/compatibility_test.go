package compatibility

import (
	"os"
	"testing"
)

func TestRepositoryManifest(t *testing.T) {
	data, err := os.ReadFile("../../COMPATIBILITY.json")
	if err != nil {
		t.Fatal(err)
	}
	pig, err := PiG(data)
	if err != nil {
		t.Fatal(err)
	}
	if pig.Path != "github.com/MichaelKinsy/PiG" || pig.Version != "0.4.1" {
		t.Fatalf("PiG = %+v", pig)
	}
}

func TestManifestValidation(t *testing.T) {
	for _, data := range []string{
		`invalid`, `{}`, `{"dependencies":{}}`,
		`{"dependencies":{"pig":{"version":"0.4.1"}}}`,
		`{"dependencies":{"pig":{"path":"github.com/MichaelKinsy/PiG"}}}`,
		`{"dependencies":{"pig":{"path":"repo","version":"latest"}}}`,
		`{"dependencies":{"pig":{"path":"repo","version":"0.4.1;sh"}}}`,
		`{"dependencies":{"pig":{"path":"repo","version":"99999999999999999999999.0.0"}}}`,
	} {
		if _, err := PiG([]byte(data)); err == nil {
			t.Errorf("accepted invalid manifest: %s", data)
		}
	}
	data := []byte(`{"dependencies":{"pig":{"path":"repo","version":"0.4.2"},"future":{"path":"another","version":"1.0.0"}}}`)
	if pig, err := PiG(data); err != nil || pig.Version != "0.4.2" {
		t.Fatalf("PiG = %+v, %v", pig, err)
	}
}
