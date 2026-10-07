// Package lancet scores shell commands with the LANCET Nano v0.4.3 CPU INT8 model: byte-level BPE tokenizer,
// windowed CodeT5+ encoder run by ONNX Runtime, then a small float32 head with Platt calibration. It is a Go port
// of smart-approve-lancet's src/lancet/{classifier,tokenizer,model-manifest}.ts and must score the same.
//
// The model (Apache-2.0) and the ONNX Runtime shared library (MIT) are never bundled; callers point at them.
package lancet

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// ModelID names the pinned model release.
const ModelID = "lancet-nano-v0.4.3"

// Archive is the pinned release archive that `setup` downloads.
var Archive = struct {
	URL    string
	Bytes  int64
	SHA256 string
	Prefix string
}{
	URL:    "https://github.com/TannerMidd/LANCET-model/releases/download/v0.4.3/lancet-v0.4.3-nano-cpu-int8.zip",
	Bytes:  108_521_974,
	SHA256: "75b7307abf16a9c63806be6a6aa88e9b18cc463cddf7e4fdc6c9b8e27d4c985c",
	Prefix: "lancet-v0.4.3-nano-cpu-int8/model/",
}

type artifact struct {
	bytes  int64
	sha256 string
}

// modelFiles are the exact artifacts of the pinned release.
var modelFiles = map[string]artifact{
	"encoder-int8.onnx": {110_550_587, "4e7d6a53d27a7321a2638a4bf446301e8e343c51000963331469e3ab20aae2a4"},
	"head.bin":          {4_730_888, "80def54b93bd97191a6e57a09164885a479cad228a3ec83f04efec32576d3c42"},
	"head.json":         {1_059, "dc4911d9cb469326cbf7a5eca7dc41328f80d9d394d10333b407345fa007cff2"},
	"vocab.json":        {703_051, "43bb485f4de0f2fd49b370bef4efab23ea3ab6d0019e72bdd9cec1436a6eaa2b"},
	"merges.txt":        {294_364, "5d346f84939a98df0cde902df7d60154b461cde1b90dc653ab9b81d74e752a4d"},
	"model.json":        {1_996, "85f6682e709a3dcfb59f2a9b3d79e83184a9deef0ef5f85f8912d369ae091b89"},
}

// readVerified reads one artifact and refuses it unless it is exactly the pinned release file.
func readVerified(dir, name string) ([]byte, error) {
	want, ok := modelFiles[name]
	if !ok {
		return nil, fmt.Errorf("unknown LANCET artifact: %s", name)
	}
	path := filepath.Join(dir, name)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("LANCET artifact %s: %w", name, err)
	}
	if !info.Mode().IsRegular() || info.Size() != want.bytes {
		return nil, fmt.Errorf("LANCET artifact is missing or the wrong size: %s", name)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read LANCET artifact %s: %w", name, err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want.sha256 {
		return nil, fmt.Errorf("LANCET artifact failed its checksum: %s", name)
	}
	return data, nil
}
