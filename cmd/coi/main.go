package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mensfeld/code-on-incus/internal/cli"
)

func main() {
	// Detect if called as 'coi' or 'claude-on-incus'
	progName := filepath.Base(os.Args[0])
	isCoi := progName == "coi"

	if err := cli.Execute(isCoi); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
