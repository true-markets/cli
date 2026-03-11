// Package main provides the entry point for the True Markets CLI.
package main

import (
	"os"

	"github.com/true-markets/cli/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
