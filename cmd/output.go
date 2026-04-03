package cmd

import (
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	platformui "github.com/ffreis/platform-bootstrap/internal/ui"
)

type commandOutput struct {
	out io.Writer
	err io.Writer
	ui  *platformui.Presenter
}

func newCommandOutput(cmd *cobra.Command, presenter *platformui.Presenter) *commandOutput {
	return &commandOutput{
		out: cmd.OutOrStdout(),
		err: cmd.ErrOrStderr(),
		ui:  presenter,
	}
}

func (o *commandOutput) Line(text string) {
	writeLine(o.out, text)
}

func (o *commandOutput) ErrLine(text string) {
	writeLine(o.err, text)
}

func (o *commandOutput) Blank() {
	writeLine(o.out, "")
}

func (o *commandOutput) Header(title, subtitle string) {
	if o.ui != nil {
		o.Line(o.ui.Header(title, subtitle))
		return
	}
	o.Line(title)
	if subtitle != "" {
		o.Line(subtitle)
	}
}

func (o *commandOutput) Summary(title string, parts ...string) {
	if o.ui != nil {
		o.Line(o.ui.Summary(title, parts...))
		return
	}
	filtered := filterParts(parts)
	if len(filtered) == 0 {
		o.Line(title)
		return
	}
	o.Line(title + ": " + strings.Join(filtered, "  "))
}

func (o *commandOutput) Status(kind, label, detail string) {
	if o.ui != nil {
		o.Line(o.ui.Status(kind, label, detail))
		return
	}
	o.Line("[" + label + "] " + detail)
}

func (o *commandOutput) ErrStatus(kind, label, detail string) {
	if o.ui != nil {
		o.ErrLine(o.ui.Status(kind, label, detail))
		return
	}
	o.ErrLine("[" + label + "] " + detail)
}

func (o *commandOutput) Bullet(label, value string) {
	o.Line("- " + label + ": " + value)
}

func (o *commandOutput) Table(headers []string, rows [][]string) error {
	w := tabwriter.NewWriter(o.out, 0, 0, 2, ' ', 0)
	_, _ = io.WriteString(w, strings.Join(headers, "\t")+"\n")
	for _, row := range rows {
		_, _ = io.WriteString(w, strings.Join(row, "\t")+"\n")
	}
	return w.Flush()
}

func (o *commandOutput) Write(data []byte) error {
	_, err := o.out.Write(data)
	return err
}

func writeLine(w io.Writer, text string) {
	_, _ = io.WriteString(w, text+"\n")
}

func filterParts(parts []string) []string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			filtered = append(filtered, part)
		}
	}
	return filtered
}

func countPart(label string, value int) string {
	return label + "=" + strconv.Itoa(value)
}

func orgRegionSummary(org, region string) string {
	return "org " + org + " in " + region
}

func auditSummary(org, accountID, region string) string {
	return "org " + org + "  account " + accountID + "  region " + region
}
