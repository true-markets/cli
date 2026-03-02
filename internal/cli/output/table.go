// Package output provides utilities for formatting CLI output.
package output

import (
	"fmt"
	"io"
	"strings"
)

// Table renders a padded-column table to an io.Writer.
type Table struct {
	Headers []string
	Rows    [][]string
}

// Render writes the table to w with padded columns.
func (t *Table) Render(w io.Writer) {
	if len(t.Headers) == 0 {
		return
	}

	// Calculate column widths.
	widths := make([]int, len(t.Headers))
	for i, h := range t.Headers {
		widths[i] = len(h)
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Print header.
	printRow(w, t.Headers, widths)

	// Print separator.
	seps := make([]string, len(widths))
	for i, w := range widths {
		seps[i] = strings.Repeat("-", w)
	}
	_, _ = fmt.Fprintln(w, strings.Join(seps, "  "))

	// Print rows.
	for _, row := range t.Rows {
		printRow(w, row, widths)
	}
}

func printRow(w io.Writer, cells []string, widths []int) {
	var parts []string
	for i, cell := range cells {
		width := 0
		if i < len(widths) {
			width = widths[i]
		}
		parts = append(parts, fmt.Sprintf("%-*s", width, cell))
	}
	_, _ = fmt.Fprintln(w, strings.Join(parts, "  "))
}
