package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	platformui "github.com/ffreis/platform-bootstrap/internal/ui"
)

func TestCommandOutputPlainHelpers(t *testing.T) {
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	out := newCommandOutput(cmd, nil)
	out.Line("hello")
	out.ErrLine("warn")
	out.Blank()
	out.Header("Title", "Subtitle")
	out.Summary("Summary", "one", "", "two")
	out.Status("ok", "ok", "done")
	out.ErrStatus("error", "fail", "bad")
	out.Bullet("org", "acme")
	if err := out.Table([]string{"A", "B"}, [][]string{{"1", "2"}}); err != nil {
		t.Fatalf("Table() unexpected error: %v", err)
	}
	if err := out.Write([]byte("tail")); err != nil {
		t.Fatalf("Write() unexpected error: %v", err)
	}

	gotOut := stdout.String()
	if !bytes.Contains([]byte(gotOut), []byte("Title\nSubtitle\n")) {
		t.Fatalf("stdout missing header, got:\n%s", gotOut)
	}
	if !bytes.Contains([]byte(gotOut), []byte("Summary: one  two\n")) {
		t.Fatalf("stdout missing summary, got:\n%s", gotOut)
	}
	if !bytes.Contains([]byte(gotOut), []byte("[ok] done\n")) {
		t.Fatalf("stdout missing status, got:\n%s", gotOut)
	}
	if !bytes.Contains([]byte(gotOut), []byte("- org: acme\n")) {
		t.Fatalf("stdout missing bullet, got:\n%s", gotOut)
	}
	if !bytes.Contains([]byte(gotOut), []byte("A  B\n1  2\n")) {
		t.Fatalf("stdout missing table, got:\n%s", gotOut)
	}
	if !bytes.HasSuffix([]byte(gotOut), []byte("tail")) {
		t.Fatalf("stdout missing write payload, got:\n%s", gotOut)
	}
	if gotErr := stderr.String(); gotErr != "warn\n[fail] bad\n" {
		t.Fatalf("stderr mismatch: %q", gotErr)
	}
}

func TestCommandOutputWithPresenter(t *testing.T) {
	presenter, err := platformui.New("plain")
	if err != nil {
		t.Fatalf("ui.New() unexpected error: %v", err)
	}

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	out := newCommandOutput(cmd, presenter)
	out.Header("Title", "Subtitle")
	out.Summary("Summary", "alpha")
	out.Status("ok", "ok", "done")

	got := stdout.String()
	for _, want := range []string{"Title", "Subtitle", "Summary: alpha", "[ok] done"} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Fatalf("output missing %q in:\n%s", want, got)
		}
	}
}

func TestTablePreservesANSIInRichMode(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	presenter, err := platformui.New(platformui.ModeRich)
	if err != nil {
		t.Fatalf("ui.New() unexpected error: %v", err)
	}

	var stdout bytes.Buffer
	out := &commandOutput{out: &stdout, err: &stdout, ui: presenter}
	rows := [][]string{{"\x1b[32mok\x1b[0m", "DynamoDBTable", "registry"}}

	if err := out.Table([]string{"STATUS", "TYPE", "NAME"}, rows); err != nil {
		t.Fatalf("Table() unexpected error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("rich table output did not preserve ANSI sequences: %q", got)
	}
	if strings.Contains(got, "[ok]") {
		t.Fatalf("rich table output fell back to plain rendering: %q", got)
	}
}

func TestOutputHelpers(t *testing.T) {
	var buf bytes.Buffer
	writeLine(&buf, "line")
	if got := buf.String(); got != "line\n" {
		t.Fatalf("writeLine() = %q", got)
	}

	filtered := filterParts([]string{"one", " ", "two"})
	if len(filtered) != 2 || filtered[0] != "one" || filtered[1] != "two" {
		t.Fatalf("filterParts() unexpected result: %#v", filtered)
	}
	if got := countPart("ok", 3); got != "ok=3" {
		t.Fatalf("countPart() = %q", got)
	}
	if got := orgRegionSummary("acme", "us-east-1"); got != "org acme in us-east-1" {
		t.Fatalf("orgRegionSummary() = %q", got)
	}
	if got := auditSummary("acme", "123", "us-east-1"); got != "org acme  account 123  region us-east-1" {
		t.Fatalf("auditSummary() = %q", got)
	}
}

func TestComputeColumnWidths(t *testing.T) {
	headers := []string{"NAME", "STATUS"}
	rows := [][]string{
		{"alice", "ok"},
		{"very-long-name", "missing"},
	}
	widths := computeColumnWidths(headers, rows)
	if len(widths) != 2 {
		t.Fatalf("expected 2 widths, got %d", len(widths))
	}
	// "very-long-name" (14) > "NAME" (4)
	if widths[0] != 14 {
		t.Errorf("col[0] width = %d, want 14", widths[0])
	}
	// "missing" (7) > "STATUS" (6)
	if widths[1] != 7 {
		t.Errorf("col[1] width = %d, want 7", widths[1])
	}
}

func TestComputeColumnWidths_ExtraColumnsIgnored(t *testing.T) {
	headers := []string{"A"}
	rows := [][]string{{"x", "extra-col-ignored"}}
	widths := computeColumnWidths(headers, rows)
	if len(widths) != 1 {
		t.Fatalf("expected 1 width, got %d", len(widths))
	}
	if widths[0] != 1 {
		t.Errorf("col[0] width = %d, want 1", widths[0])
	}
}

func TestCommandOutputSummaryWithoutParts(t *testing.T) {
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)

	out := newCommandOutput(cmd, nil)
	out.Summary("Summary")

	if got := stdout.String(); got != "Summary\n" {
		t.Fatalf("Summary() = %q", got)
	}
}

func TestWriteTableRowIgnoresExtraColumns(t *testing.T) {
	var stdout bytes.Buffer
	out := &commandOutput{out: &stdout, err: &stdout}
	out.writeTableRow([]string{"left", "right", "ignored"}, []int{4, 5})

	if got := stdout.String(); got != "left  right\n" {
		t.Fatalf("writeTableRow() = %q", got)
	}
}
