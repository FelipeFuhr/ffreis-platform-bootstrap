package ui

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestResolveMode(t *testing.T) {
	t.Parallel()

	mode, interactive, err := ResolveMode("auto", true, false, false)
	if err != nil {
		t.Fatalf("ResolveMode() error: %v", err)
	}
	if mode != ModeRich || !interactive {
		t.Fatalf("ResolveMode() = (%q, %v), want (%q, true)", mode, interactive, ModeRich)
	}
}

func TestResolveModeInvalid(t *testing.T) {
	t.Parallel()

	if _, _, err := ResolveMode("broken", true, true, false); err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func TestWithPresenterRoundTrip(t *testing.T) {
	t.Parallel()

	presenter, err := New("plain")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	ctx := WithPresenter(context.Background(), presenter)
	if got := FromContext(ctx); got != presenter {
		t.Fatal("expected presenter round-trip")
	}
}

func TestPresenterPlainHelpers(t *testing.T) {
	t.Parallel()

	p := &Presenter{mode: ModePlain}
	if got := p.Badge("ok", "OK"); got != "[ok]" {
		t.Fatalf("Badge() = %q", got)
	}
	if got := p.Duration(1300 * time.Millisecond); got != "1.3s" {
		t.Fatalf("Duration() = %q", got)
	}
	if got := p.Status("warn", "STEP", "creating bucket"); !strings.Contains(got, "[step]") {
		t.Fatalf("Status() = %q", got)
	}
}
