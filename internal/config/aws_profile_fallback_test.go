package config

import "testing"

func TestLoad_AWSProfileFallback_AWSProfileEnv(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvAWSProfile, "")
	t.Setenv(AWSProfileEnv, "sso-profile")
	t.Setenv(AWSDefaultProfileEnv, "")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AWSProfile != "sso-profile" {
		t.Fatalf("AWSProfile: got %q, want %q", cfg.AWSProfile, "sso-profile")
	}
}

func TestLoad_AWSProfileFallback_AWSDefaultProfileEnv(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvAWSProfile, "")
	t.Setenv(AWSProfileEnv, "")
	t.Setenv(AWSDefaultProfileEnv, "default-profile")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AWSProfile != "default-profile" {
		t.Fatalf("AWSProfile: got %q, want %q", cfg.AWSProfile, "default-profile")
	}
}

func TestLoad_AWSProfileFallback_DoesNotOverridePlatformProfile(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvAWSProfile, "platform-profile")
	t.Setenv(AWSProfileEnv, "sso-profile")
	t.Setenv(AWSDefaultProfileEnv, "default-profile")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AWSProfile != "platform-profile" {
		t.Fatalf("AWSProfile: got %q, want %q", cfg.AWSProfile, "platform-profile")
	}
}
