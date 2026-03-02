// Package main provides the entry point for the external DeFi CLI.
package main

import (
	"os"

	"github.com/true-markets/defi-cli/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
