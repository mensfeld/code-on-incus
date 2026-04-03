package main

import (
	"errors"
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
		var exitErr *cli.ExitCodeError
		if errors.As(err, &exitErr) {
			if exitErr.Message != "" {
				fmt.Fprintln(os.Stderr, exitErr.Message)
			}
			os.Exit(exitErr.Code)
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
