package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCmd_DefaultsToDevUnknown(t *testing.T) {
	// Do not run in parallel: this test mutates package-scoped variables.
	oldVersion, oldCommit, oldBuildTime := version, commit, buildTime
	t.Cleanup(func() {
		version, commit, buildTime = oldVersion, oldCommit, oldBuildTime
	})

	version = ""
	commit = ""
	buildTime = ""

	var out bytes.Buffer
	versionCmd.SetOut(&out)
	versionCmd.SetErr(&out)

	versionCmd.Run(versionCmd, nil)

	got := out.String()
	want := "dev (commit=unknown built=unknown)\n"
	if got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}

func TestVersionCmd_TrimsWhitespace(t *testing.T) {
	// Do not run in parallel: this test mutates package-scoped variables.
	oldVersion, oldCommit, oldBuildTime := version, commit, buildTime
	t.Cleanup(func() {
		version, commit, buildTime = oldVersion, oldCommit, oldBuildTime
	})

	version = " v1.2.3 \n"
	commit = "\tabc123\n"
	buildTime = " 2026-03-27T12:34:56Z "

	var out bytes.Buffer
	versionCmd.SetOut(&out)
	versionCmd.SetErr(&out)

	versionCmd.Run(versionCmd, nil)

	got := strings.TrimSpace(out.String())
	want := "v1.2.3 (commit=abc123 built=2026-03-27T12:34:56Z)"
	if got != want {
		t.Fatalf("output: got %q, want %q", got, want)
	}
}
