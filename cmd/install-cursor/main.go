// Command install-cursor installs only the pinned, verified Cursor SDK Bridge.
package main

import (
	"context"
	"fmt"
	"github.com/VBenevides/pig-plugins/internal/agentdir"
	"github.com/VBenevides/pig-plugins/internal/cursorbridge"
	"os"
	"runtime"
	"time"
)

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: install-cursor")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	path, err := cursorbridge.Install(ctx, agentdir.Dir(os.Getenv), runtime.GOOS, runtime.GOARCH)
	if err != nil {
		fmt.Fprintln(os.Stderr, "install Cursor:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "install: Cursor SDK Bridge at", path)
}
