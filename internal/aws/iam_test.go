package aws

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

// mockIAM is a stateful stand-in for IAMAPI. roleExists transitions to true
// after CreateRole, mirroring real AWS behaviour.
type mockIAM struct {
	roleExists      bool
	getRoleErr      error
	createRoleErr   error
	createRoleCalls int
	putPolicyCalls  int
	tagRoleCalls    int
	tagRoleErr      error
	policyNames     []string
	listPoliciesErr error
	deletePolicyErr error
	deleteRoleErr   error
}

func (m *mockIAM) GetAccountSummary(_ context.Context, _ *iam.GetAccountSummaryInput, _ ...func(*iam.Options)) (*iam.GetAccountSummaryOutput, error) {
	return &iam.GetAccountSummaryOutput{}, nil
}

func (m *mockIAM) GetRole(_ context.Context, _ *iam.GetRoleInput, _ ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
	if m.getRoleErr != nil {
		return nil, m.getRoleErr
	}
	if m.roleExists {
		return &iam.GetRoleOutput{Role: &iamtypes.Role{RoleName: sdkaws.String(testRoleName)}}, nil
	}
	return nil, &iamtypes.NoSuchEntityException{}
}

func (m *mockIAM) CreateRole(_ context.Context, params *iam.CreateRoleInput, _ ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	m.createRoleCalls++
	if m.createRoleErr != nil {
		return nil, m.createRoleErr
	}
	m.roleExists = true
	return &iam.CreateRoleOutput{
		Role: &iamtypes.Role{RoleName: params.RoleName},
	}, nil
}

func (m *mockIAM) PutRolePolicy(_ context.Context, _ *iam.PutRolePolicyInput, _ ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
	m.putPolicyCalls++
	return &iam.PutRolePolicyOutput{}, nil
}

func (m *mockIAM) TagRole(_ context.Context, _ *iam.TagRoleInput, _ ...func(*iam.Options)) (*iam.TagRoleOutput, error) {
	m.tagRoleCalls++
	return &iam.TagRoleOutput{}, m.tagRoleErr
}

func (m *mockIAM) ListRolePolicies(_ context.Context, _ *iam.ListRolePoliciesInput, _ ...func(*iam.Options)) (*iam.ListRolePoliciesOutput, error) {
	if m.listPoliciesErr != nil {
		return nil, m.listPoliciesErr
	}
	return &iam.ListRolePoliciesOutput{PolicyNames: m.policyNames}, nil
}

func (m *mockIAM) DeleteRolePolicy(_ context.Context, _ *iam.DeleteRolePolicyInput, _ ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
	return &iam.DeleteRolePolicyOutput{}, m.deletePolicyErr
}

func (m *mockIAM) DeleteRole(_ context.Context, _ *iam.DeleteRoleInput, _ ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
	if m.deleteRoleErr != nil {
		return nil, m.deleteRoleErr
	}
	m.roleExists = false
	return &iam.DeleteRoleOutput{}, nil
}

// Temp-user stubs — not exercised by iam_test.go but required by IAMAPI.
func (m *mockIAM) GetUser(_ context.Context, _ *iam.GetUserInput, _ ...func(*iam.Options)) (*iam.GetUserOutput, error) {
	return nil, &iamtypes.NoSuchEntityException{}
}
func (m *mockIAM) CreateUser(_ context.Context, _ *iam.CreateUserInput, _ ...func(*iam.Options)) (*iam.CreateUserOutput, error) {
	return &iam.CreateUserOutput{User: &iamtypes.User{}}, nil
}
func (m *mockIAM) PutUserPolicy(_ context.Context, _ *iam.PutUserPolicyInput, _ ...func(*iam.Options)) (*iam.PutUserPolicyOutput, error) {
	return &iam.PutUserPolicyOutput{}, nil
}
func (m *mockIAM) CreateAccessKey(_ context.Context, _ *iam.CreateAccessKeyInput, _ ...func(*iam.Options)) (*iam.CreateAccessKeyOutput, error) {
	return &iam.CreateAccessKeyOutput{AccessKey: &iamtypes.AccessKey{}}, nil
}
func (m *mockIAM) ListAccessKeys(_ context.Context, _ *iam.ListAccessKeysInput, _ ...func(*iam.Options)) (*iam.ListAccessKeysOutput, error) {
	return &iam.ListAccessKeysOutput{}, nil
}
func (m *mockIAM) DeleteAccessKey(_ context.Context, _ *iam.DeleteAccessKeyInput, _ ...func(*iam.Options)) (*iam.DeleteAccessKeyOutput, error) {
	return &iam.DeleteAccessKeyOutput{}, nil
}
func (m *mockIAM) DeleteUserPolicy(_ context.Context, _ *iam.DeleteUserPolicyInput, _ ...func(*iam.Options)) (*iam.DeleteUserPolicyOutput, error) {
	return &iam.DeleteUserPolicyOutput{}, nil
}
func (m *mockIAM) DeleteUser(_ context.Context, _ *iam.DeleteUserInput, _ ...func(*iam.Options)) (*iam.DeleteUserOutput, error) {
	return &iam.DeleteUserOutput{}, nil
}

// TestEnsurePlatformAdminRole_Create verifies that when the role does not
// exist, CreateRole and PutRolePolicy are both called.
func TestEnsurePlatformAdminRole_Create(t *testing.T) {
	m := &mockIAM{}

	if err := EnsurePlatformAdminRole(context.Background(), m, testRoleName, "123456789012", nil); err != nil {
		t.Fatalf(errUnexpectedFmt, err)
	}

	if m.createRoleCalls != 1 {
		t.Errorf("createRoleCalls: want 1, got %d", m.createRoleCalls)
	}
	if m.putPolicyCalls != 1 {
		t.Errorf("putPolicyCalls: want 1, got %d", m.putPolicyCalls)
	}
	if m.tagRoleCalls != 0 {
		t.Errorf("tagRoleCalls: want 0 (nil tags), got %d", m.tagRoleCalls)
	}
}

// TestEnsurePlatformAdminRole_AlreadyExists verifies that when the role
// already exists, CreateRole is skipped but PutRolePolicy is always called
// (so policy changes on re-run take effect).
func TestEnsurePlatformAdminRole_AlreadyExists(t *testing.T) {
	m := &mockIAM{roleExists: true}

	if err := EnsurePlatformAdminRole(context.Background(), m, testRoleName, "123456789012", nil); err != nil {
		t.Fatalf(errUnexpectedFmt, err)
	}

	if m.createRoleCalls != 0 {
		t.Errorf("createRoleCalls: want 0 (role existed), got %d", m.createRoleCalls)
	}
	if m.putPolicyCalls != 1 {
		t.Errorf("putPolicyCalls: want 1 (always applied), got %d", m.putPolicyCalls)
	}
}

// TestEnsurePlatformAdminRole_EntityAlreadyExists verifies that a concurrent
// CreateRole (EntityAlreadyExistsException) is treated as success.
func TestEnsurePlatformAdminRole_EntityAlreadyExists(t *testing.T) {
	m := &mockIAM{
		createRoleErr: &iamtypes.EntityAlreadyExistsException{},
	}

	if err := EnsurePlatformAdminRole(context.Background(), m, testRoleName, "123456789012", nil); err != nil {
		t.Fatalf("expected EntityAlreadyExists to be treated as success, got: %v", err)
	}

	if m.putPolicyCalls != 1 {
		t.Errorf("putPolicyCalls: want 1 (policy applied even after concurrent create), got %d", m.putPolicyCalls)
	}
}

// TestEnsurePlatformAdminRole_Idempotent is the core idempotency test:
// calling EnsurePlatformAdminRole twice must result in exactly one CreateRole.
func TestEnsurePlatformAdminRole_Idempotent(t *testing.T) {
	m := &mockIAM{}

	// First call — role does not exist.
	if err := EnsurePlatformAdminRole(context.Background(), m, testRoleName, "123456789012", nil); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if m.createRoleCalls != 1 {
		t.Fatalf("after first call: createRoleCalls want 1, got %d", m.createRoleCalls)
	}

	// Second call — role now exists (mock state updated by CreateRole).
	if err := EnsurePlatformAdminRole(context.Background(), m, testRoleName, "123456789012", nil); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if m.createRoleCalls != 1 {
		t.Errorf("after second call: createRoleCalls want 1 (no new create), got %d", m.createRoleCalls)
	}
	// PutRolePolicy is called on every run so policy changes are applied.
	if m.putPolicyCalls != 2 {
		t.Errorf("putPolicyCalls: want 2 (once per run), got %d", m.putPolicyCalls)
	}
}

// TestEnsurePlatformAdminRole_TrustPolicy verifies the trust document encodes
// the account root ARN and sts:AssumeRole correctly.
func TestEnsurePlatformAdminRole_TrustPolicy(t *testing.T) {
	capture := &trustCapturingIAM{}

	accountID := "123456789012"
	if err := EnsurePlatformAdminRole(context.Background(), capture, testRoleName, accountID, nil); err != nil {
		t.Fatalf(errUnexpectedFmt, err)
	}

	raw := capture.lastTrustDoc
	if raw == "" {
		t.Fatal("CreateRole was not called")
	}

	var doc policyDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("trust policy is not valid JSON: %v", err)
	}

	if len(doc.Statement) != 1 {
		t.Fatalf("trust policy: want 1 statement, got %d", len(doc.Statement))
	}

	stmt := doc.Statement[0]
	if stmt.Effect != "Allow" {
		t.Errorf("trust effect: want Allow, got %s", stmt.Effect)
	}

	wantARN := "arn:aws:iam::" + accountID + ":root"
	principal, ok := stmt.Principal.(map[string]interface{})
	if !ok || principal["AWS"] != wantARN {
		t.Errorf("trust principal: want {AWS: %s}, got %v", wantARN, stmt.Principal)
	}
}

// TestEnsurePlatformAdminRole_DenyList verifies the permissions policy
// includes the deny statement and covers at least the critical root actions.
func TestEnsurePlatformAdminRole_DenyList(t *testing.T) {
	capture := &policyCapturingIAM{}

	if err := EnsurePlatformAdminRole(context.Background(), capture, testRoleName, "123456789012", nil); err != nil {
		t.Fatalf(errUnexpectedFmt, err)
	}

	raw := capture.lastPolicyDoc
	if raw == "" {
		t.Fatal("PutRolePolicy was not called")
	}

	mustContain := []string{
		"iam:CreateVirtualMFADevice",
		"iam:DeleteVirtualMFADevice",
		"account:CloseAccount",
	}
	for _, action := range mustContain {
		if !strings.Contains(raw, action) {
			t.Errorf("permissions policy: missing expected deny action %q", action)
		}
	}

	if !strings.Contains(raw, `"Effect":"Deny"`) {
		t.Error("permissions policy: missing Deny statement")
	}
}

// TestEnsurePlatformAdminRole_TagsApplied verifies that when tags are
// provided, TagRole is called.
func TestEnsurePlatformAdminRole_TagsApplied(t *testing.T) {
	m := &mockIAM{}
	tags := map[string]string{"Project": "platform", "Owner": "ffreis"}

	if err := EnsurePlatformAdminRole(context.Background(), m, testRoleName, "123456789012", tags); err != nil {
		t.Fatalf(errUnexpectedFmt, err)
	}

	if m.tagRoleCalls != 1 {
		t.Errorf("tagRoleCalls: want 1, got %d", m.tagRoleCalls)
	}
}

// ---- capture helpers ----

type trustCapturingIAM struct {
	mockIAM
	lastTrustDoc string
}

func (c *trustCapturingIAM) CreateRole(_ context.Context, params *iam.CreateRoleInput, _ ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	c.lastTrustDoc = sdkaws.ToString(params.AssumeRolePolicyDocument)
	c.mockIAM.roleExists = true
	return &iam.CreateRoleOutput{Role: &iamtypes.Role{RoleName: params.RoleName}}, nil
}

func (c *trustCapturingIAM) TagRole(_ context.Context, _ *iam.TagRoleInput, _ ...func(*iam.Options)) (*iam.TagRoleOutput, error) {
	return &iam.TagRoleOutput{}, nil
}

type policyCapturingIAM struct {
	mockIAM
	lastPolicyDoc string
}

func (c *policyCapturingIAM) PutRolePolicy(_ context.Context, params *iam.PutRolePolicyInput, _ ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
	c.lastPolicyDoc = sdkaws.ToString(params.PolicyDocument)
	c.mockIAM.putPolicyCalls++
	return &iam.PutRolePolicyOutput{}, nil
}

func (c *policyCapturingIAM) TagRole(_ context.Context, _ *iam.TagRoleInput, _ ...func(*iam.Options)) (*iam.TagRoleOutput, error) {
	return &iam.TagRoleOutput{}, nil
}
