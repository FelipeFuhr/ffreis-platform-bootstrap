package aws

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteAWSProfile writes or updates an AWS credentials profile in ~/.aws/credentials.
// Creates the ~/.aws directory and credentials file if they don't exist.
// The file is rewritten entirely from the parsed profile map; formatting,
// comments, and blank lines from the original file are not preserved.
//
// Format written:
//
//	[profileName]
//	aws_access_key_id = accessKeyID
//	aws_secret_access_key = secretAccessKey
//	region = region
func WriteAWSProfile(profileName, accessKeyID, secretAccessKey, region string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	awsDir := filepath.Join(homeDir, ".aws")
	credentialsPath := filepath.Join(awsDir, "credentials")

	// Create ~/.aws directory if it doesn't exist (mode 0700 for security)
	if err := os.MkdirAll(awsDir, 0700); err != nil {
		return fmt.Errorf("creating ~/.aws directory: %w", err)
	}

	// Read existing credentials file if present
	sections, err := parseCredentialsFile(credentialsPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading credentials file: %w", err)
	}

	// Update or add the profile
	sections[profileName] = map[string]string{
		"aws_access_key_id":     accessKeyID,
		"aws_secret_access_key": secretAccessKey,
		"region":                region,
	}

	// Write back the credentials file
	if err := writeCredentialsFile(credentialsPath, sections); err != nil {
		return fmt.Errorf("writing credentials file: %w", err)
	}

	return nil
}

// parseCredentialsFile reads an AWS credentials file and returns a map of
// profile name -> key/value pairs.
func parseCredentialsFile(path string) (map[string]map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return make(map[string]map[string]string), err
	}
	defer file.Close()

	sections := make(map[string]map[string]string)
	var currentSection string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// Section header: [profile-name]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.TrimPrefix(strings.TrimSuffix(line, "]"), "[")
			if sections[currentSection] == nil {
				sections[currentSection] = make(map[string]string)
			}
			continue
		}

		// Key = value
		if currentSection != "" && strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				sections[currentSection][key] = value
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return sections, nil
}

// writeCredentialsFile writes the credentials sections to the file atomically.
// It writes to a temporary file in the same directory, then renames it over
// the destination to avoid leaving a partially-written file on crash or
// disk-full errors. File permissions are set to 0600 for security.
func writeCredentialsFile(path string, sections map[string]map[string]string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp credentials file: %w", err)
	}
	tmpName := tmp.Name()

	// Clean up the temp file on any error before rename.
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpName)
		}
	}()

	if err := os.Chmod(tmpName, 0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting temp file permissions: %w", err)
	}

	first := true
	for name, values := range sections {
		if !first {
			if _, err := fmt.Fprintln(tmp); err != nil {
				_ = tmp.Close()
				return fmt.Errorf("writing credentials file: %w", err)
			}
		}
		first = false

		if _, err := fmt.Fprintf(tmp, "[%s]\n", name); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("writing credentials file: %w", err)
		}
		for key, value := range values {
			if _, err := fmt.Fprintf(tmp, "%s = %s\n", key, value); err != nil {
				_ = tmp.Close()
				return fmt.Errorf("writing credentials file: %w", err)
			}
		}
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("flushing temp credentials file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("renaming temp credentials file: %w", err)
	}

	success = true
	return nil
}
