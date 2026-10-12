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
