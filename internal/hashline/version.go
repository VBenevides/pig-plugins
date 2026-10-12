package hashline

import (
	"crypto/sha256"
	"encoding/hex"
)

// ContentVersion identifies exact file bytes, including BOM and line terminators.
// Short display anchors are not suitable for version validation.
func ContentVersion(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func checkVersion(value any, content []byte) error {
	version, ok := value.(string)
	if !ok || len(version) != 64 {
		return editErrorf("version must be the 64-character token returned by read. Read the file again.")
	}
	if _, err := hex.DecodeString(version); err != nil {
		return editErrorf("version must be the 64-character token returned by read. Read the file again.")
	}
	if version != ContentVersion(content) {
		return categorizedEditError(CategoryStale, "stale version: the file changed since it was read. Read it again; nothing was edited.")
	}
	return nil
}

func withVersion(result Result, content []byte) Result {
	version := ContentVersion(content)
	if result.Details == nil {
		result.Details = map[string]any{}
	}
	result.Details["version"] = version
	result.Text += "\nVersion: " + version
	return result
}
