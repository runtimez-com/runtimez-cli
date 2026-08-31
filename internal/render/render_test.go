package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseFormat(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Format
		err  bool
	}{
		{"", FormatTable, false},
		{"table", FormatTable, false},
		{"WIDE", FormatWide, false},
		{" json ", FormatJSON, false},
		{"yaml", FormatYAML, false},
		{"xml", "", true},
	} {
		got, err := ParseFormat(tc.in)
		if tc.err {
			if err == nil {
				t.Errorf("ParseFormat(%q) accepted an unsupported format", tc.in)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ParseFormat(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestTableRendersHeadersAndRows(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{Out: &buf, Format: FormatTable}
	err := p.Print(nil, &Table{
		Headers: []string{"NAME", "STATUS"},
		Rows:    [][]string{{"prod", "CONNECTED"}, {"stage", "STALE"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"NAME", "STATUS", "prod", "CONNECTED", "stage", "STALE"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestEmptyTableSaysSo(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{Out: &buf, Format: FormatTable}
	if err := p.Print(nil, &Table{Headers: []string{"NAME"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No resources found") {
		t.Errorf("an empty result printed bare headers:\n%s", buf.String())
	}
}

func TestWideAppendsExtraColumns(t *testing.T) {
	table := &Table{
		Headers:     []string{"NAME"},
		Rows:        [][]string{{"prod"}},
		WideHeaders: []string{"PROVIDER"},
		WideRows:    [][]string{{"eks"}},
	}

	var narrow bytes.Buffer
	if err := (&Printer{Out: &narrow, Format: FormatTable}).Print(nil, table); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(narrow.String(), "PROVIDER") {
		t.Errorf("table format leaked a wide column:\n%s", narrow.String())
	}

	var wide bytes.Buffer
	if err := (&Printer{Out: &wide, Format: FormatWide}).Print(nil, table); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wide.String(), "PROVIDER") || !strings.Contains(wide.String(), "eks") {
		t.Errorf("wide format dropped the extra column:\n%s", wide.String())
	}

	// Rendering wide must not mutate the caller's table — the TUI reuses one across refreshes.
	if len(table.Headers) != 1 || len(table.Rows[0]) != 1 {
		t.Errorf("wide rendering mutated the source table: %+v", table)
	}
}

// jq must work without a .data prefix, so the JSON path emits the payload the command was
// handed and nothing wrapping it.
func TestJSONEmitsUnwrappedPayload(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{Out: &buf, Format: FormatJSON}
	data := []map[string]string{{"id": "c1"}}
	if err := p.Print(data, &Table{Headers: []string{"ID"}, Rows: [][]string{{"c1"}}}); err != nil {
		t.Fatal(err)
	}

	var got []map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output was not a bare JSON array: %v\n%s", err, buf.String())
	}
	if len(got) != 1 || got[0]["id"] != "c1" {
		t.Errorf("payload changed shape: %+v", got)
	}
	if strings.Contains(buf.String(), "success") || strings.Contains(buf.String(), `"data"`) {
		t.Errorf("envelope leaked into -o json:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "ID") {
		t.Errorf("table headers leaked into -o json:\n%s", buf.String())
	}
}

func TestYAMLEmitsPayload(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{Out: &buf, Format: FormatYAML}
	if err := p.Print(map[string]string{"id": "c1"}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "id: c1") {
		t.Errorf("unexpected yaml:\n%s", buf.String())
	}
}

func TestDash(t *testing.T) {
	if got := Dash("  "); got != "<none>" {
		t.Errorf("Dash(blank) = %q", got)
	}
	if got := Dash("prod"); got != "prod" {
		t.Errorf("Dash(value) = %q", got)
	}
}

func TestStructured(t *testing.T) {
	if !FormatJSON.Structured() || !FormatYAML.Structured() {
		t.Error("json/yaml should be structured")
	}
	if FormatTable.Structured() || FormatWide.Structured() {
		t.Error("table/wide should not be structured")
	}
}

// "No resources found" is wrong for a latency window with no traffic; a wrong empty state
// sends someone looking for the wrong problem.
func TestEmptyMessageOverridesTheDefault(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{Out: &buf, Format: FormatTable}
	err := p.Print(nil, &Table{
		Headers:      []string{"SERVICE"},
		EmptyMessage: "No traced operations in this window.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No traced operations") {
		t.Errorf("custom empty message ignored:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "No resources found") {
		t.Errorf("default empty message still printed:\n%s", buf.String())
	}
}

func TestEmptyMessageDefaultsWhenUnset(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{Out: &buf, Format: FormatTable}
	if err := p.Print(nil, &Table{Headers: []string{"NAME"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No resources found") {
		t.Errorf("default empty message missing:\n%s", buf.String())
	}
}
