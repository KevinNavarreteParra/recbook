// Command recbook finds library audiobooks similar to books I already liked.
//
// This file only starts the CLI; each subcommand lives in internal/cli.
package main

import (
	"os"

	"github.com/KevinNavarreteParra/recbook/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
