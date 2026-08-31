// Package render turns a command's result into terminal output. Every command produces both
// a raw payload and a table; this package picks which one the caller asked for, so adding a
// column never risks breaking `-o json` and vice versa.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// Format is the -o value.
type Format string

const (
	FormatTable Format = "table"
	FormatWide  Format = "wide"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
)

// ParseFormat validates an -o value.
func ParseFormat(s string) (Format, error) {
	switch Format(strings.ToLower(strings.TrimSpace(s))) {
	case FormatTable, "":
		return FormatTable, nil
	case FormatWide:
		return FormatWide, nil
	case FormatJSON:
		return FormatJSON, nil
	case FormatYAML:
		return FormatYAML, nil
	default:
		return "", fmt.Errorf("unsupported output format %q (want table, wide, json or yaml)", s)
	}
}

// Structured reports whether the format is machine-readable, in which case a command must
// not decorate the output with headings or hints.
func (f Format) Structured() bool { return f == FormatJSON || f == FormatYAML }

// Table is the human view: fixed headers plus already-stringified cells.
type Table struct {
	Headers []string
	Rows    [][]string
	// WideHeaders/WideRows are appended when -o wide is requested. Leave nil when a
	// resource has nothing extra to show.
	WideHeaders []string
	WideRows    [][]string
	// EmptyMessage replaces the default when there are no rows. "No resources found" is
	// wrong for a latency window with no traffic, and a wrong empty-state sends someone
	// looking for the wrong problem.
	EmptyMessage string
}

// Printer writes one command's result.
type Printer struct {
	Out    io.Writer
	Format Format
}

// New builds a printer over stdout.
func New(format Format) *Printer { return &Printer{Out: os.Stdout, Format: format} }

// Print renders data as JSON/YAML, or the table for the human formats.
//
// The JSON path emits the unwrapped payload — never the ApiResponse envelope — so `jq` works
// without a `.data` prefix.
func (p *Printer) Print(data any, table *Table) error {
	switch p.Format {
	case FormatJSON:
		enc := json.NewEncoder(p.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	case FormatYAML:
		enc := yaml.NewEncoder(p.Out)
		enc.SetIndent(2)
		if err := enc.Encode(data); err != nil {
			return err
		}
		return enc.Close()
	default:
		if table == nil {
			return nil
		}
		return p.printTable(table)
	}
}

func (p *Printer) printTable(t *Table) error {
	headers := t.Headers
	rows := t.Rows
	if p.Format == FormatWide && len(t.WideHeaders) > 0 {
		headers = append(append([]string{}, headers...), t.WideHeaders...)
		merged := make([][]string, len(rows))
		for i, r := range rows {
			extra := []string{}
			if i < len(t.WideRows) {
				extra = t.WideRows[i]
			}
			merged[i] = append(append([]string{}, r...), extra...)
		}
		rows = merged
	}

	if len(rows) == 0 {
		msg := t.EmptyMessage
		if msg == "" {
			msg = "No resources found."
		}
		fmt.Fprintln(p.Out, msg)
		return nil
	}

	w := tabwriter.NewWriter(p.Out, 0, 0, 3, ' ', 0)
	if len(headers) > 0 {
		fmt.Fprintln(w, strings.Join(headers, "\t"))
	}
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	return w.Flush()
}

// Dash renders an empty value the way kubectl does, so a blank column is never ambiguous
// with a missing one.
func Dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "<none>"
	}
	return s
}
