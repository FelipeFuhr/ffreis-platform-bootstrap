package aws

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	platformcfg "github.com/ffreis/platform-bootstrap/internal/config"
)

// mockSTS implements CallerIdentityProvider for verifyIdentity tests.
type mockSTS struct {
	out *sts.GetCallerIdentityOutput
	err error
}

func (m *mockSTS) GetCallerIdentity(_ context.Context, _ *sts.GetCallerIdentityInput, _ ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.out, nil
}

// mockCredentialsProvider implements sdkaws.CredentialsProvider for tests.
type mockCredentialsProvider struct {
	creds sdkaws.Credentials
	err   error
}

func (m *mockCredentialsProvider) Retrieve(ctx context.Context) (sdkaws.Credentials, error) {
	if m.err != nil {
		return sdkaws.Credentials{}, m.err
	}
	return m.creds, nil
}

// MockCredentialsProvider is exported for use in other test packages.
// It allows tests to simulate credential failures or specific credential values.
type MockCredentialsProvider struct {
	Creds sdkaws.Credentials
	Err   error
}

func (m *MockCredentialsProvider) Retrieve(ctx context.Context) (sdkaws.Credentials, error) {
	if m.Err != nil {
		return sdkaws.Credentials{}, m.Err
	}
	return m.Creds, nil
}

func TestNewNoCredentials(t *testing.T) {
	cfg := &platformcfg.Config{Region: testRegion}

	// Use mock provider that fails to retrieve credentials
	mockProvider := &MockCredentialsProvider{
		Err: errors.New("no credentials available"),
	}

	_, err := loadConfigWithOpts(context.Background(), cfg, mockProvider)
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("loadConfigWithOpts() without credentials: want ErrNoCredentials, got %v", err)
	}
}

func TestLoadConfigEnvCredentials(t *testing.T) {
	cfg := &platformcfg.Config{Region: testRegion}
	t.Setenv("AWS_ACCESS_KEY_ID", "test-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")

	awsCfg, err := loadConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("loadConfig() unexpected error: %v", err)
	}
	if awsCfg.Region != testRegion {
		t.Fatalf("Region: want us-east-1, got %s", awsCfg.Region)
	}
}

func TestLoadConfigProfileNotFound(t *testing.T) {
	cfg := &platformcfg.Config{Region: testRegion, AWSProfile: "profile-that-should-not-exist"}
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")

	_, err := loadConfig(context.Background(), cfg)
	if err == nil {
		t.Fatal("loadConfig() expected error for missing profile, got nil")
	}
	if !strings.Contains(err.Error(), "loading AWS config") {
		t.Fatalf("loadConfig() error should be wrapped with context, got: %v", err)
	}
}

func TestNewWithOptsMissingCredentials(t *testing.T) {
	cfg := &platformcfg.Config{Region: testRegion}

	// Use mock provider that fails to retrieve credentials
	mockProvider := &MockCredentialsProvider{
		Err: errors.New("no credentials available"),
	}

	_, err := NewWithOpts(context.Background(), cfg, mockProvider)
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("NewWithOpts() without credentials: want ErrNoCredentials, got %v", err)
	}
}

func TestVerifyIdentitySuccess(t *testing.T) {
	c := &Clients{STS: &mockSTS{out: &sts.GetCallerIdentityOutput{
		Account: sdkaws.String("123456789012"),
		Arn:     sdkaws.String(testBootstrapARN),
	}}}

	err := c.verifyIdentity(context.Background())
	if err != nil {
		t.Fatalf("verifyIdentity() unexpected error: %v", err)
	}
	if c.AccountID != "123456789012" {
		t.Fatalf("AccountID: want 123456789012, got %s", c.AccountID)
	}
	if c.CallerARN != testBootstrapARN {
		t.Fatalf("CallerARN not populated correctly: %s", c.CallerARN)
	}
}

func TestVerifyIdentityError(t *testing.T) {
	c := &Clients{STS: &mockSTS{err: errors.New("sts failure")}}

	err := c.verifyIdentity(context.Background())
	if err == nil {
		t.Fatal("verifyIdentity() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "verifying AWS credentials") {
		t.Fatalf("verifyIdentity() should wrap error context, got: %v", err)
	}
}

func TestNewSuccessWithLocalSTS(t *testing.T) {
	stsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <GetCallerIdentityResult>
    <Arn>arn:aws:iam::123456789012:user/bootstrap</Arn>
    <Account>123456789012</Account>
    <UserId>AIDTEST</UserId>
  </GetCallerIdentityResult>
  <ResponseMetadata>
    <RequestId>req-1</RequestId>
  </ResponseMetadata>
</GetCallerIdentityResponse>`))
	}))
	defer stsServer.Close()

	cfg := &platformcfg.Config{Region: testRegion}
	t.Setenv("AWS_ACCESS_KEY_ID", "test-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	// Service-specific endpoint override keeps the test fully local.
	t.Setenv("AWS_ENDPOINT_URL_STS", stsServer.URL)

	clients, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	if clients.AccountID != "123456789012" {
		t.Fatalf("AccountID: want 123456789012, got %s", clients.AccountID)
	}
	if clients.CallerARN != testBootstrapARN {
		t.Fatalf("CallerARN: unexpected value %s", clients.CallerARN)
	}
	if clients.Region != testRegion {
		t.Fatalf("Region: want us-east-1, got %s", clients.Region)
	}
}
