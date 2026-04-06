package cmd

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRunNukeAllStepBranches(t *testing.T) {
	t.Run("empty command", func(t *testing.T) {
		err := runNukeAllStep(context.Background(), bootstrapNukeAllStep{label: "empty"}, os.Stdout, os.Stderr)
		if err == nil || !strings.Contains(err.Error(), "empty command") {
			t.Fatalf("expected empty command error, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		step := bootstrapNukeAllStep{
			label:   "success",
			workdir: t.TempDir(),
			command: []string{"sh", "-c", "exit 0"},
		}
		if err := runNukeAllStep(context.Background(), step, os.Stdout, os.Stderr); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("failure", func(t *testing.T) {
		step := bootstrapNukeAllStep{
			label:   "boom",
			workdir: t.TempDir(),
			command: []string{"sh", "-c", "exit 7"},
		}
		err := runNukeAllStep(context.Background(), step, os.Stdout, os.Stderr)
		if err == nil || !strings.Contains(err.Error(), "boom failed") {
			t.Fatalf("expected wrapped step error, got %v", err)
		}
	})
}

func TestBootstrapRepoRootFindsGitDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir() unexpected error: %v", err)
	}
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll() unexpected error: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Chdir(nested); err != nil {
		t.Fatalf("Chdir() unexpected error: %v", err)
	}

	got, err := bootstrapRepoRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != root {
		t.Fatalf("bootstrapRepoRoot() = %q, want %q", got, root)
	}
}

func TestBootstrapRepoRootFailsOutsideGitRepo(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("Chdir() unexpected error: %v", err)
	}

	_, err = bootstrapRepoRoot()
	if err == nil || !strings.Contains(err.Error(), "not inside a git repository") {
		t.Fatalf("expected git repo error, got %v", err)
	}
}

func TestPreflightBootstrapNukeAllSuccess(t *testing.T) {
	platformRoot := t.TempDir()
	repoRoot := filepath.Join(platformRoot, "ffreis-platform-bootstrap")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() unexpected error: %v", err)
	}
	writeNukeFixture(t, filepath.Join(platformRoot, "ffreis-platform-atlantis", "stack"), "")
	writeNukeFixture(t, filepath.Join(platformRoot, "ffreis-platform-atlantis", "envs", "prod", "backend.hcl"), "bucket = \"b\"\nkey = \"k\"\nregion = \"r\"\n")

	writeNukeFixture(t, filepath.Join(platformRoot, "ffreis-platform-project-template", "stack"), "")
	writeNukeFixture(t, filepath.Join(platformRoot, "ffreis-platform-project-template", "envs", "prod", "backend.hcl"), "bucket = \"b\"\nkey = \"k\"\nregion = \"r\"\ndynamodb_table = \"locks\"\n")
	writeNukeFixture(t, filepath.Join(platformRoot, "ffreis-platform-project-template", "envs", "prod", "fetched.auto.tfvars.json"), "{}\n")

	writeNukeFixture(t, filepath.Join(platformRoot, "ffreis-platform-github-oidc", "stack"), "")
	writeNukeFixture(t, filepath.Join(platformRoot, "ffreis-platform-github-oidc", "envs", "prod", "config.local.yaml"), "role: platform-admin\n")
	writeNukeFixture(t, filepath.Join(platformRoot, "ffreis-platform-github-oidc", "envs", "prod", "backend.local.hcl"), "bucket = \"b\"\nkey = \"k\"\nregion = \"r\"\ndynamodb_table = \"locks\"\n")

	writeNukeFixture(t, filepath.Join(platformRoot, "ffreis-platform-org", "terraform", "stack"), "")
	writeNukeFixture(t, filepath.Join(platformRoot, "ffreis-platform-org", "terraform", "envs", "prod", "terraform.tfvars"), "org = \"acme\"\n")
	writeNukeFixture(t, filepath.Join(platformRoot, "ffreis-platform-org", "terraform", "envs", "prod", "fetched.auto.tfvars.json"), "{}\n")

	if err := preflightBootstrapNukeAll(repoRoot, "prod"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreflightProjectTemplateNukeRejectsPlaceholder(t *testing.T) {
	platformRoot := t.TempDir()
	writeNukeFixture(t, filepath.Join(platformRoot, "ffreis-platform-project-template", "stack"), "")
	writeNukeFixture(t, filepath.Join(platformRoot, "ffreis-platform-project-template", "envs", "prod", "backend.hcl"), "bucket = \"b\"\nkey = \"{ACCOUNT_ID}\"\nregion = \"r\"\ndynamodb_table = \"locks\"\n")
	writeNukeFixture(t, filepath.Join(platformRoot, "ffreis-platform-project-template", "envs", "prod", "fetched.auto.tfvars.json"), "{}\n")

	err := preflightProjectTemplateNuke(platformRoot, "prod")
	if err == nil || !strings.Contains(err.Error(), "still contains placeholder") {
		t.Fatalf("expected placeholder error, got %v", err)
	}
}

func TestRequireDirFileAndBackendKeysHelpers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() unexpected error: %v", err)
	}
	filePath := filepath.Join(dir, "backend.hcl")
	content := "# comment\nbucket = \"bucket\"\nkey=\"state\"\n\n"
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}

	if err := requireDir(dir); err != nil {
		t.Fatalf("requireDir() unexpected error: %v", err)
	}
	read, err := requireFileText(filePath)
	if err != nil {
		t.Fatalf("requireFileText() unexpected error: %v", err)
	}
	if read != content {
		t.Fatalf("requireFileText() = %q, want %q", read, content)
	}

	missing := missingBackendKeys(content, "bucket", "key", "region")
	if !slices.Equal(missing, []string{"region"}) {
		t.Fatalf("missingBackendKeys() = %v", missing)
	}

	if err := requireDir(filePath); err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("expected not-a-directory error, got %v", err)
	}
	if _, err := requireFileText(filepath.Join(dir, "missing.hcl")); err == nil {
		t.Fatal("expected missing file error")
	}
}

func writeNukeFixture(t *testing.T, path, content string) {
	t.Helper()
	if filepath.Ext(path) == "" {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll() unexpected error: %v", err)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() unexpected error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}
}
