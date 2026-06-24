package cli

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
)

// Table provides consistent tabular output formatting for CLI commands.
type Table struct {
	headers []string
	rows    [][]string
	out     io.Writer
}

// NewTable creates a new Table with the given column headers, writing to os.Stdout.
func NewTable(headers ...string) *Table {
	return &Table{
		headers: headers,
		out:     os.Stdout,
	}
}

// SetOutput redirects the table's output to w (the default is os.Stdout). Useful
// for rendering into a buffer or an alternate writer — the overview command
// renders into the writer it was handed, and tests capture into a buffer.
func (t *Table) SetOutput(w io.Writer) {
	t.out = w
}

// AddRow appends a row. Short rows are padded with empty strings.
func (t *Table) AddRow(values ...string) {
	for len(values) < len(t.headers) {
		values = append(values, "")
	}
	t.rows = append(t.rows, values)
}

// Render writes the table via tabwriter using the same parameters as the
// existing profile list command: minwidth=0, tabwidth=4, padding=2.
func (t *Table) Render() {
	w := tabwriter.NewWriter(t.out, 0, 4, 2, ' ', 0)
	// Header
	for i, h := range t.headers {
		if i > 0 {
			fmt.Fprint(w, "\t")
		}
		fmt.Fprint(w, h)
	}
	fmt.Fprintln(w)
	// Rows
	for _, row := range t.rows {
		for i, v := range row {
			if i > 0 {
				fmt.Fprint(w, "\t")
			}
			fmt.Fprint(w, v)
		}
		fmt.Fprintln(w)
	}
	w.Flush()
}
