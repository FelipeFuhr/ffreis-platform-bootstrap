package ui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResolveMode_Branches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		requested  string
		stdoutTTY  bool
		stderrTTY  bool
		noColor    bool
		wantMode   string
		wantPrompt bool
	}{
		{name: "auto non interactive", requested: "", wantMode: ModePlain, wantPrompt: false},
		{name: "auto rich when tty", requested: ModeAuto, stdoutTTY: true, wantMode: ModeRich, wantPrompt: true},
		{name: "auto plain when no color", requested: ModeAuto, stderrTTY: true, noColor: true, wantMode: ModePlain, wantPrompt: true},
		{name: "plain explicit", requested: ModePlain, wantMode: ModePlain, wantPrompt: true},
		{name: "rich explicit", requested: ModeRich, wantMode: ModeRich, wantPrompt: true},
		{name: "rich forced plain by no color", requested: ModeRich, noColor: true, wantMode: ModePlain, wantPrompt: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, interactive, err := ResolveMode(tc.requested, tc.stdoutTTY, tc.stderrTTY, tc.noColor)
			if err != nil {
				t.Fatalf("ResolveMode() error: %v", err)
			}
			if mode != tc.wantMode || interactive != tc.wantPrompt {
				t.Fatalf("ResolveMode() = (%q, %v), want (%q, %v)", mode, interactive, tc.wantMode, tc.wantPrompt)
			}
		})
	}
}

func TestFromContext_DefaultPresenter(t *testing.T) {
	t.Parallel()

	p := FromContext(context.Background())
	if p == nil {
		t.Fatal("expected fallback presenter")
	}
	if got := p.Badge("ok", "done"); got != "[done]" {
		t.Fatalf("fallback presenter should be plain, got badge %q", got)
	}
}

func TestPresenterInteractiveAndHeaderSummary(t *testing.T) {
	t.Parallel()

	rich, err := New(ModeRich)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if !rich.Interactive() {
		t.Fatal("rich presenter should be interactive")
	}
	if got := rich.Header("Bootstrap", "ready"); !strings.Contains(got, "Bootstrap") || !strings.Contains(got, "ready") {
		t.Fatalf("Header() = %q", got)
	}
	if got := rich.Summary("summary", "", "first", "  ", "second"); !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Fatalf("Summary() = %q", got)
	}

	plain := &Presenter{mode: ModePlain}
	if got := plain.Header("Bootstrap", "ready"); got != "Bootstrap\nready" {
		t.Fatalf("plain Header() = %q", got)
	}
	if got := plain.Summary("summary"); got != "summary" {
		t.Fatalf("plain Summary() = %q", got)
	}
}

func TestPresenterBadgeStatusDurationAndRender(t *testing.T) {
	t.Parallel()

	rich, err := New(ModeRich)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if got := rich.Badge("missing", "Info"); !strings.Contains(got, "info") {
		t.Fatalf("Badge() = %q", got)
	}
	if got := rich.Status("warn", "step", "creating bucket"); !strings.Contains(got, "creating bucket") {
		t.Fatalf("Status() = %q", got)
	}
	if got := rich.Duration(345 * time.Millisecond); got != "350ms" {
		t.Fatalf("Duration() = %q", got)
	}
	if got := rich.Duration(0); got != "0s" {
		t.Fatalf("Duration() zero = %q", got)
	}
	if got := rich.render("value", rich.header); !strings.Contains(got, "value") {
		t.Fatalf("render() = %q", got)
	}
}

func TestIsTTYWithNilAndRegularFile(t *testing.T) {
	t.Parallel()

	if IsTTY(nil) {
		t.Fatal("nil file should not be tty")
	}

	f, err := os.CreateTemp(t.TempDir(), "ui-isatty-*")
	if err != nil {
		t.Fatalf("CreateTemp() error: %v", err)
	}
	defer f.Close()

	if IsTTY(f) {
		t.Fatal("regular temp file should not be tty")
	}
}
