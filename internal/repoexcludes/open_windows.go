//go:build windows

package repoexcludes

import "os"

// os.Root bounds link traversal. readExclude checks the opened file's identity.
func openExclude(root *os.Root) (*os.File, error) {
	return root.Open("exclude")
}
