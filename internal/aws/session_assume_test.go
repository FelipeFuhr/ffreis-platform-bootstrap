package aws

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
)

type mockRoleAssumer struct {
	out       *sts.AssumeRoleOutput
	err       error
	lastInput *sts.AssumeRoleInput
}

const (
	testPlatformAdminRoleARN = "arn:aws:iam::123456789012:role/platform-admin"
	errUnexpectedSTSAction   = "unexpected STS action %q"
)

func (m *mockRoleAssumer) AssumeRole(_ context.Context, params *sts.AssumeRoleInput, _ ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	m.lastInput = params
	if m.err != nil {
		return nil, m.err
	}
	return m.out, nil
}

func TestIsRootARN(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		arn  string
		want bool
	}{
		{name: "root", arn: "arn:aws:iam::123456789012:root", want: true},
		{name: "user", arn: testBootstrapARN, want: false},
		{name: "role", arn: testCallerRoleARN, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsRootARN(tc.arn); got != tc.want {
				t.Fatalf("IsRootARN(%q) = %v, want %v", tc.arn, got, tc.want)
			}
		})
	}
}

func TestVerifyIdentityProfileAddsSSOHint(t *testing.T) {
	t.Parallel()

	c := &Clients{
		STS:     &mockSTS{err: errors.New("sts failure")},
		Profile: "bootstrap-sso",
	}

	err := c.verifyIdentity(context.Background())
	if err == nil {
		t.Fatal("verifyIdentity() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "aws sso login --profile bootstrap-sso") {
		t.Fatalf("verifyIdentity() should include SSO hint, got: %v", err)
	}
}

func TestAssumeAdminRoleAssumeError(t *testing.T) {
	t.Parallel()

	orig := &Clients{
		STSRoleAssumer: &mockRoleAssumer{err: errors.New("assume boom")},
		Region:         testRegion,
		Profile:        "bootstrap",
		AccountID:      "123456789012",
	}

	_, err := AssumeAdminRole(context.Background(), orig, testPlatformAdminRoleARN)
	if err == nil {
		t.Fatal("AssumeAdminRole() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "assuming role") {
		t.Fatalf("AssumeAdminRole() should wrap assume error, got: %v", err)
	}
}

func TestAssumeAdminRoleSuccess(t *testing.T) {
	server := newSTSTestServer(t, func(action string, _ string, w http.ResponseWriter) {
		switch action {
		case "GetCallerIdentity":
			writeSTSResponse(w, `<?xml version="1.0" encoding="UTF-8"?>
<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <GetCallerIdentityResult>
    <Arn>arn:aws:sts::123456789012:assumed-role/platform-admin/platform-bootstrap-init</Arn>
    <Account>123456789012</Account>
    <UserId>AROATEST:platform-bootstrap-init</UserId>
  </GetCallerIdentityResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</GetCallerIdentityResponse>`)
		default:
			t.Fatalf(errUnexpectedSTSAction, action)
		}
	})
	defer server.Close()

	t.Setenv("AWS_ENDPOINT_URL_STS", server.URL)

	assumer := &mockRoleAssumer{
		out: &sts.AssumeRoleOutput{
			Credentials: &ststypes.Credentials{
				AccessKeyId:     sdkaws.String("ASIAEXAMPLE"),
				SecretAccessKey: sdkaws.String("secret"),
				SessionToken:    sdkaws.String("token"),
				Expiration:      sdkaws.Time(time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)),
			},
		},
	}
	orig := &Clients{
		STSRoleAssumer: assumer,
		Region:         testRegion,
		Profile:        "bootstrap",
		AccountID:      "123456789012",
	}

	got, err := AssumeAdminRole(context.Background(), orig, testPlatformAdminRoleARN)
	if err != nil {
		t.Fatalf("AssumeAdminRole() unexpected error: %v", err)
	}
	if got.AccountID != orig.AccountID {
		t.Fatalf("AccountID: want %s, got %s", orig.AccountID, got.AccountID)
	}
	if got.Profile != orig.Profile {
		t.Fatalf("Profile: want %s, got %s", orig.Profile, got.Profile)
	}
	if got.Region != orig.Region {
		t.Fatalf("Region: want %s, got %s", orig.Region, got.Region)
	}
	if !strings.Contains(got.CallerARN, "assumed-role/platform-admin") {
		t.Fatalf("CallerARN: unexpected value %q", got.CallerARN)
	}
	if assumer.lastInput == nil || sdkaws.ToString(assumer.lastInput.RoleSessionName) != "platform-bootstrap-init" {
		t.Fatalf("RoleSessionName: unexpected input %+v", assumer.lastInput)
	}
}

func TestAssumeRoleWithTempUserRetriesPropagationThenSucceeds(t *testing.T) {
	var assumeCalls atomic.Int32
	server := newSTSTestServer(t, func(action string, _ string, w http.ResponseWriter) {
		switch action {
		case "AssumeRole":
			if assumeCalls.Add(1) == 1 {
				writeSTSError(w, http.StatusForbidden, "AccessDenied", "not propagated yet")
				return
			}
			writeSTSResponse(w, `<?xml version="1.0" encoding="UTF-8"?>
<AssumeRoleResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <AssumeRoleResult>
    <Credentials>
      <AccessKeyId>ASIASECOND</AccessKeyId>
      <SecretAccessKey>secret-2</SecretAccessKey>
      <SessionToken>token-2</SessionToken>
      <Expiration>2030-01-01T00:00:00Z</Expiration>
    </Credentials>
    <AssumedRoleUser>
      <Arn>arn:aws:sts::123456789012:assumed-role/platform-admin/platform-bootstrap-init</Arn>
      <AssumedRoleId>AROATEST:platform-bootstrap-init</AssumedRoleId>
    </AssumedRoleUser>
  </AssumeRoleResult>
  <ResponseMetadata><RequestId>req-2</RequestId></ResponseMetadata>
</AssumeRoleResponse>`)
		case "GetCallerIdentity":
			writeSTSResponse(w, `<?xml version="1.0" encoding="UTF-8"?>
<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <GetCallerIdentityResult>
    <Arn>arn:aws:sts::123456789012:assumed-role/platform-admin/platform-bootstrap-init</Arn>
    <Account>123456789012</Account>
    <UserId>AROATEST:platform-bootstrap-init</UserId>
  </GetCallerIdentityResult>
  <ResponseMetadata><RequestId>req-3</RequestId></ResponseMetadata>
</GetCallerIdentityResponse>`)
		default:
			t.Fatalf(errUnexpectedSTSAction, action)
		}
	})
	defer server.Close()

	t.Setenv("AWS_ENDPOINT_URL_STS", server.URL)

	origDelays := tempUserAssumeRetryDelays
	tempUserAssumeRetryDelays = []time.Duration{time.Millisecond}
	t.Cleanup(func() { tempUserAssumeRetryDelays = origDelays })

	orig := &Clients{
		Region:    testRegion,
		Profile:   "bootstrap",
		AccountID: "123456789012",
	}
	u := TempUser{
		UserName:        TempBootstrapUserName,
		AccessKeyID:     "AKIATEMP",
		SecretAccessKey: "secret",
	}

	got, err := AssumeRoleWithTempUser(context.Background(), orig, u, testPlatformAdminRoleARN)
	if err != nil {
		t.Fatalf("AssumeRoleWithTempUser() unexpected error: %v", err)
	}
	if assumeCalls.Load() != 2 {
		t.Fatalf("assume role calls: want 2, got %d", assumeCalls.Load())
	}
	if !strings.Contains(got.CallerARN, "assumed-role/platform-admin") {
		t.Fatalf("CallerARN: unexpected value %q", got.CallerARN)
	}
}

func TestAssumeRoleWithTempUserContextCancelledDuringRetry(t *testing.T) {
	attempted := make(chan struct{}, 1)
	server := newSTSTestServer(t, func(action string, _ string, w http.ResponseWriter) {
		switch action {
		case "AssumeRole":
			select {
			case attempted <- struct{}{}:
			default:
			}
			writeSTSError(w, http.StatusForbidden, "AccessDenied", "not propagated yet")
		case "GetCallerIdentity":
			t.Fatalf("GetCallerIdentity should not be called when assume role keeps failing")
		default:
			t.Fatalf(errUnexpectedSTSAction, action)
		}
	})
	defer server.Close()

	t.Setenv("AWS_ENDPOINT_URL_STS", server.URL)

	origDelays := tempUserAssumeRetryDelays
	tempUserAssumeRetryDelays = []time.Duration{time.Second}
	t.Cleanup(func() { tempUserAssumeRetryDelays = origDelays })

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-attempted
		cancel()
	}()

	_, err := AssumeRoleWithTempUser(ctx, &Clients{
		Region:    testRegion,
		Profile:   "bootstrap",
		AccountID: "123456789012",
	}, TempUser{
		UserName:        TempBootstrapUserName,
		AccessKeyID:     "AKIATEMP",
		SecretAccessKey: "secret",
	}, testPlatformAdminRoleARN)
	if err == nil {
		t.Fatal("AssumeRoleWithTempUser() expected error, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "context canceled") {
		t.Fatalf("AssumeRoleWithTempUser() should report cancellation, got: %v", err)
	}
}

func newSTSTestServer(t *testing.T, handler func(action, body string, w http.ResponseWriter)) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		body := string(bodyBytes)
		action := extractSTSAction(r, body)
		if action == "" {
			t.Fatalf("unable to determine STS action from query=%q body=%q", r.URL.RawQuery, body)
		}
		handler(action, body, w)
	}))
}

func extractSTSAction(r *http.Request, body string) string {
	if action := r.URL.Query().Get("Action"); action != "" {
		return action
	}
	for _, part := range strings.Split(body, "&") {
		if strings.HasPrefix(part, "Action=") {
			return strings.TrimPrefix(part, "Action=")
		}
	}
	return ""
}

func writeSTSResponse(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/xml")
	_, _ = w.Write([]byte(body))
}

func writeSTSError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ErrorResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <Error>
    <Type>Sender</Type>
    <Code>` + code + `</Code>
    <Message>` + message + `</Message>
  </Error>
  <RequestId>req-error</RequestId>
</ErrorResponse>`))
}
