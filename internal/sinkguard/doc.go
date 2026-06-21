// Package sinkguard holds a regression guard (in its test file) ensuring that
// packages whose code runs while a `coi shell` session is interactively attached
// never write to a terminal sink (os.Stdout/os.Stderr/global log/fmt.Print*).
//
// During an attached session the coi process's os.Stdout/os.Stderr ARE the
// user's tmux terminal, so any such write corrupts the AI tool's TUI — the
// recurring "issue #372" class. Diagnostics from those packages must go to the
// file-based SessionLogger (or a package logger that routes to it) instead.
//
// The package intentionally contains no runtime code; see guard_test.go.
package sinkguard
