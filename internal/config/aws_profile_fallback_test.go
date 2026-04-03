package config

import "testing"

const (
	testSSOProfile       = "sso-profile"
	testDefaultProfile   = "default-profile"
	testPlatformProfile  = "platform-profile"
	errLoadConfig        = "Load: %v"
	errUnexpectedProfile = "AWSProfile: got %q, want %q"
)

func TestLoadAWSProfileFallbackAWSProfileEnv(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvAWSProfile, "")
	t.Setenv(AWSProfileEnv, testSSOProfile)
	t.Setenv(AWSDefaultProfileEnv, "")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(errLoadConfig, err)
	}
	if cfg.AWSProfile != testSSOProfile {
		t.Fatalf(errUnexpectedProfile, cfg.AWSProfile, testSSOProfile)
	}
}

func TestLoadAWSProfileFallbackAWSDefaultProfileEnv(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvAWSProfile, "")
	t.Setenv(AWSProfileEnv, "")
	t.Setenv(AWSDefaultProfileEnv, testDefaultProfile)

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(errLoadConfig, err)
	}
	if cfg.AWSProfile != testDefaultProfile {
		t.Fatalf(errUnexpectedProfile, cfg.AWSProfile, testDefaultProfile)
	}
}

func TestLoadAWSProfileFallbackDoesNotOverridePlatformProfile(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvAWSProfile, testPlatformProfile)
	t.Setenv(AWSProfileEnv, testSSOProfile)
	t.Setenv(AWSDefaultProfileEnv, testDefaultProfile)

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(errLoadConfig, err)
	}
	if cfg.AWSProfile != testPlatformProfile {
		t.Fatalf(errUnexpectedProfile, cfg.AWSProfile, testPlatformProfile)
	}
}
