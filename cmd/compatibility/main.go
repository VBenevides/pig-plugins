// Command compatibility prints the supported PiG version for build/install scripts.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/VBenevides/pig-plugins/internal/compatibility"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: compatibility <COMPATIBILITY.json>")
		os.Exit(2)
	}
	if err := printVersion(os.Args[1], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printVersion(path string, output io.Writer) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read compatibility manifest: %w", err)
	}
	pig, err := compatibility.PiG(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, pig.Version)
	return err
}
