package aws

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setHome points os.UserHomeDir() at a throwaway directory and returns the
// ~/.aws/credentials path inside it.
//
// WriteAWSProfile resolves the home directory itself rather than taking a path,
// so this is what makes it testable without changing production code:
// os.UserHomeDir() reads $HOME on unix. Tests using this CANNOT call
// t.Parallel() — t.Setenv panics when the test is parallel, by design, because
// the environment is process-global.
func setHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return filepath.Join(dir, ".aws", "credentials")
}

// TestWriteAWSProfileCreatesNewFile verifies that WriteAWSProfile creates
// ~/.aws and the credentials file when neither exists yet, with the restrictive
// permissions credentials require.
func TestWriteAWSProfileCreatesNewFile(t *testing.T) {
	credPath := setHome(t)

	if err := WriteAWSProfile("myprofile", "AKIATEST", "secret", "us-east-1"); err != nil {
		t.Fatalf("WriteAWSProfile() unexpected error: %v", err)
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

	// Long-lived AWS keys: the file must not be group/world readable, and the
	// directory must not be traversable by others.
	info, err := os.Stat(credPath)
	if err != nil {
		t.Fatalf("stat credentials file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("credentials file perm: want 0600, got %04o", perm)
	}
	dirInfo, err := os.Stat(filepath.Dir(credPath))
	if err != nil {
		t.Fatalf("stat .aws dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0700 {
		t.Errorf(".aws dir perm: want 0700, got %04o", perm)
	}
}

// TestWriteAWSProfileUpdatesExistingProfile verifies that re-writing a profile
// replaces its credentials in place rather than appending a duplicate section.
func TestWriteAWSProfileUpdatesExistingProfile(t *testing.T) {
	credPath := setHome(t)

	if err := WriteAWSProfile("target", "AKIA_OLD", "old-secret", "us-east-1"); err != nil {
		t.Fatalf("WriteAWSProfile() initial: %v", err)
	}
	if err := WriteAWSProfile("target", "AKIA_NEW", "new-secret", "us-east-1"); err != nil {
		t.Fatalf("WriteAWSProfile() update: %v", err)
	}

	sections, err := parseCredentialsFile(credPath)
	if err != nil {
		t.Fatalf("parseCredentialsFile(): %v", err)
	}
	if got := sections["target"]["aws_access_key_id"]; got != "AKIA_NEW" {
		t.Errorf("target profile not updated: got %q, want AKIA_NEW", got)
	}
	if got := sections["target"]["aws_secret_access_key"]; got != "new-secret" {
		t.Errorf("target secret not updated: got %q", got)
	}

	// A duplicated [target] section would still parse into one map entry, so
	// assert on the raw file too — the stale key must be gone, not shadowed.
	data, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatalf("reading credentials file: %v", err)
	}
	if n := strings.Count(string(data), "[target]"); n != 1 {
		t.Errorf("want exactly 1 [target] section, got %d:\n%s", n, data)
	}
	if strings.Contains(string(data), "AKIA_OLD") {
		t.Errorf("superseded credential still present in file:\n%s", data)
	}
}

// TestWriteAWSProfilePreservesOtherProfiles verifies that writing one profile
// leaves unrelated profiles in the same file untouched — the file is shared
// with every other AWS tool on the machine.
func TestWriteAWSProfilePreservesOtherProfiles(t *testing.T) {
	credPath := setHome(t)

	if err := WriteAWSProfile("alpha", "AKIA_A", "secret-a", "us-east-1"); err != nil {
		t.Fatalf("WriteAWSProfile(alpha): %v", err)
	}
	if err := WriteAWSProfile("beta", "AKIA_B", "secret-b", "us-west-2"); err != nil {
		t.Fatalf("WriteAWSProfile(beta): %v", err)
	}

	parsed, err := parseCredentialsFile(credPath)
	if err != nil {
		t.Fatalf("parseCredentialsFile(): %v", err)
	}
	if len(parsed) != 2 {
		t.Errorf("want 2 profiles, got %d: %v", len(parsed), parsed)
	}
	if got := parsed["alpha"]["aws_access_key_id"]; got != "AKIA_A" {
		t.Errorf("alpha profile clobbered: got %q", got)
	}
	if got := parsed["alpha"]["region"]; got != "us-east-1" {
		t.Errorf("alpha region clobbered: got %q", got)
	}
	if got := parsed["beta"]["aws_access_key_id"]; got != "AKIA_B" {
		t.Errorf("beta profile: got %q", got)
	}
}

// TestWriteAWSProfileAppendsToPreExistingFile verifies the read-modify-write
// path against a credentials file this tool did not create — the common real
// case, where the user already has profiles from the AWS CLI.
func TestWriteAWSProfileAppendsToPreExistingFile(t *testing.T) {
	credPath := setHome(t)

	if err := os.MkdirAll(filepath.Dir(credPath), 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	preexisting := "[default]\naws_access_key_id = AKIA_PREEXISTING\naws_secret_access_key = untouched\nregion = sa-east-1\n"
	if err := os.WriteFile(credPath, []byte(preexisting), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := WriteAWSProfile("added", "AKIA_ADDED", "added-secret", "us-east-1"); err != nil {
		t.Fatalf("WriteAWSProfile(): %v", err)
	}

	parsed, err := parseCredentialsFile(credPath)
	if err != nil {
		t.Fatalf("parseCredentialsFile(): %v", err)
	}
	if got := parsed["default"]["aws_access_key_id"]; got != "AKIA_PREEXISTING" {
		t.Errorf("pre-existing default profile was modified: got %q", got)
	}
	if got := parsed["default"]["region"]; got != "sa-east-1" {
		t.Errorf("pre-existing default region was modified: got %q", got)
	}
	if got := parsed["added"]["aws_access_key_id"]; got != "AKIA_ADDED" {
		t.Errorf("new profile not written: got %q", got)
	}
}

// TestWriteAWSProfileFailsWhenHomeIsUnresolvable verifies the error is reported
// rather than swallowed. A silent failure here would leave the caller believing
// credentials were persisted when they were not.
func TestWriteAWSProfileFailsWhenHomeIsUnresolvable(t *testing.T) {
	t.Setenv("HOME", "")

	err := WriteAWSProfile("myprofile", "AKIATEST", "secret", "us-east-1")
	if err == nil {
		t.Fatal("WriteAWSProfile() with no resolvable home: want error, got nil")
	}
	if !strings.Contains(err.Error(), "home directory") {
		t.Errorf("error = %q, want it to name the home-directory lookup", err)
	}
}

// TestWriteAWSProfileFailsWhenAWSDirIsAFile verifies the MkdirAll failure path:
// if ~/.aws exists but is a regular file, creating the directory cannot succeed
// and the error must surface.
func TestWriteAWSProfileFailsWhenAWSDirIsAFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Occupy ~/.aws with a regular file so MkdirAll cannot create the directory.
	if err := os.WriteFile(filepath.Join(dir, ".aws"), []byte("not a directory"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := WriteAWSProfile("myprofile", "AKIATEST", "secret", "us-east-1")
	if err == nil {
		t.Fatal("WriteAWSProfile() with ~/.aws as a file: want error, got nil")
	}
	if !strings.Contains(err.Error(), ".aws directory") {
		t.Errorf("error = %q, want it to name the .aws directory creation", err)
	}
}

// TestWriteAWSProfileFailsOnUnreadableCredentials verifies that a read failure
// on an EXISTING credentials file aborts instead of silently starting from an
// empty set — which would drop every profile already in the file.
func TestWriteAWSProfileFailsOnUnreadableCredentials(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not restrict access")
	}

	dir := t.TempDir()
	t.Setenv("HOME", dir)

	awsDir := filepath.Join(dir, ".aws")
	if err := os.MkdirAll(awsDir, 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	credPath := filepath.Join(awsDir, "credentials")
	if err := os.WriteFile(credPath, []byte("[default]\n"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Chmod(credPath, 0000); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(credPath, 0600) })

	err := WriteAWSProfile("myprofile", "AKIATEST", "secret", "us-east-1")
	if err == nil {
		t.Fatal("WriteAWSProfile() with an unreadable credentials file: want error, got nil")
	}
	if !strings.Contains(err.Error(), "credentials file") {
		t.Errorf("error = %q, want it to name the credentials file read", err)
	}
}

// TestCredentialsFileRoundTrip covers the write/parse helpers directly, without
// going through WriteAWSProfile's home-directory resolution.
func TestCredentialsFileRoundTrip(t *testing.T) {
	t.Parallel()

	credPath := filepath.Join(t.TempDir(), ".aws", "credentials")
	if err := os.MkdirAll(filepath.Dir(credPath), 0700); err != nil {
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
	for name, want := range sections {
		for key, wantVal := range want {
			if got := parsed[name][key]; got != wantVal {
				t.Errorf("profile %q key %q: got %q, want %q", name, key, got, wantVal)
			}
		}
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
