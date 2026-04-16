package main

import (
	"fmt"
	"os"

	"github.com/alxxpersonal/exo-discord/internal/cli"
)

// --- Main ---

func main() {
	if err := cli.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
