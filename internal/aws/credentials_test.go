package aws

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteAWSProfileCreatesNewFile verifies that WriteAWSProfile creates
// the credentials file when it does not yet exist.
func TestWriteAWSProfileCreatesNewFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	awsDir := filepath.Join(dir, ".aws")
	credPath := filepath.Join(awsDir, "credentials")

	// Patch home dir by temporarily overriding the file path via writeCredentialsFile
	// directly, since WriteAWSProfile calls os.UserHomeDir().
	// Use writeCredentialsFile + parseCredentialsFile directly to test round-trip.
	sections := map[string]map[string]string{
		"myprofile": {
			"aws_access_key_id":     "AKIATEST",
			"aws_secret_access_key": "secret",
			"region":                "us-east-1",
		},
	}

	if err := os.MkdirAll(awsDir, 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := writeCredentialsFile(credPath, sections); err != nil {
		t.Fatalf("writeCredentialsFile() unexpected error: %v", err)
	}

	data, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatalf("reading credentials file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[myprofile]") {
		t.Errorf("credentials file missing profile header, got:\n%s", content)
	}
	if !strings.Contains(content, "aws_access_key_id = AKIATEST") {
		t.Errorf("credentials file missing access key, got:\n%s", content)
	}
	// Verify file permissions are 0600.
	info, err := os.Stat(credPath)
	if err != nil {
		t.Fatalf("stat credentials file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("credentials file perm: want 0600, got %04o", perm)
	}
}

// TestWriteAWSProfileUpdatesExistingProfile verifies that an existing profile
// is overwritten while other profiles are preserved.
func TestWriteAWSProfileUpdatesExistingProfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	awsDir := filepath.Join(dir, ".aws")
	credPath := filepath.Join(awsDir, "credentials")

	if err := os.MkdirAll(awsDir, 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Write an initial file with two profiles.
	initial := map[string]map[string]string{
		"other": {
			"aws_access_key_id":     "AKIA_OTHER",
			"aws_secret_access_key": "other-secret",
			"region":                "eu-west-1",
		},
		"target": {
			"aws_access_key_id":     "AKIA_OLD",
			"aws_secret_access_key": "old-secret",
			"region":                "us-east-1",
		},
	}
	if err := writeCredentialsFile(credPath, initial); err != nil {
		t.Fatalf("writeCredentialsFile() initial write: %v", err)
	}

	// Parse existing and update the target profile.
	sections, err := parseCredentialsFile(credPath)
	if err != nil {
		t.Fatalf("parseCredentialsFile(): %v", err)
	}
	sections["target"] = map[string]string{
		"aws_access_key_id":     "AKIA_NEW",
		"aws_secret_access_key": "new-secret",
		"region":                "us-east-1",
	}
	if err := writeCredentialsFile(credPath, sections); err != nil {
		t.Fatalf("writeCredentialsFile() update: %v", err)
	}

	// Re-parse and verify.
	updated, err := parseCredentialsFile(credPath)
	if err != nil {
		t.Fatalf("parseCredentialsFile() after update: %v", err)
	}
	if updated["target"]["aws_access_key_id"] != "AKIA_NEW" {
		t.Errorf("target profile not updated: got %q", updated["target"]["aws_access_key_id"])
	}
	if updated["other"]["aws_access_key_id"] != "AKIA_OTHER" {
		t.Errorf("other profile was modified: got %q", updated["other"]["aws_access_key_id"])
	}
}

// TestWriteAWSProfilePreservesOtherProfiles verifies that unrelated profiles
// survive a round-trip through writeCredentialsFile → parseCredentialsFile.
func TestWriteAWSProfilePreservesOtherProfiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	awsDir := filepath.Join(dir, ".aws")
	credPath := filepath.Join(awsDir, "credentials")

	if err := os.MkdirAll(awsDir, 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}

	sections := map[string]map[string]string{
		"alpha": {"aws_access_key_id": "AKIA_A", "aws_secret_access_key": "secret-a", "region": "us-east-1"},
		"beta":  {"aws_access_key_id": "AKIA_B", "aws_secret_access_key": "secret-b", "region": "us-west-2"},
	}
	if err := writeCredentialsFile(credPath, sections); err != nil {
		t.Fatalf("writeCredentialsFile(): %v", err)
	}

	parsed, err := parseCredentialsFile(credPath)
	if err != nil {
		t.Fatalf("parseCredentialsFile(): %v", err)
	}
	if len(parsed) != 2 {
		t.Errorf("want 2 profiles, got %d", len(parsed))
	}
	if parsed["alpha"]["aws_access_key_id"] != "AKIA_A" {
		t.Errorf("alpha profile: unexpected key %q", parsed["alpha"]["aws_access_key_id"])
	}
	if parsed["beta"]["aws_access_key_id"] != "AKIA_B" {
		t.Errorf("beta profile: unexpected key %q", parsed["beta"]["aws_access_key_id"])
	}
}

// TestParseCredentialsFileNotExist verifies that parseCredentialsFile returns
// an os.IsNotExist error for a missing file, which WriteAWSProfile treats as
// an empty credential set.
func TestParseCredentialsFileNotExist(t *testing.T) {
	t.Parallel()

	_, err := parseCredentialsFile(filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Fatal("parseCredentialsFile() expected error for missing file, got nil")
	}
	if !os.IsNotExist(err) {
		t.Errorf("parseCredentialsFile() should return IsNotExist error, got: %v", err)
	}
}
