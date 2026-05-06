package aws

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

// adminUserIAMMock is a configurable mock for CreateAdminUser tests.
type adminUserIAMMock struct {
	mockIAM

	getUserOut            *iam.GetUserOutput
	getUserErr            error
	createUserErr         error
	putUserPolicyErr      error
	createAccessKeyOut    *iam.CreateAccessKeyOutput
	createAccessKeyErr    error
	listAccessKeysOut     *iam.ListAccessKeysOutput
	listAccessKeysErr     error
	deleteAccessKeyInputs []*iam.DeleteAccessKeyInput
	deleteAccessKeyErr    error
}

func (m *adminUserIAMMock) GetUser(_ context.Context, _ *iam.GetUserInput, _ ...func(*iam.Options)) (*iam.GetUserOutput, error) {
	if m.getUserErr != nil {
		return nil, m.getUserErr
	}
	if m.getUserOut != nil {
		return m.getUserOut, nil
	}
	return nil, &iamtypes.NoSuchEntityException{}
}

func (m *adminUserIAMMock) CreateUser(_ context.Context, params *iam.CreateUserInput, _ ...func(*iam.Options)) (*iam.CreateUserOutput, error) {
	if m.createUserErr != nil {
		return nil, m.createUserErr
	}
	return &iam.CreateUserOutput{User: &iamtypes.User{UserName: params.UserName}}, nil
}

func (m *adminUserIAMMock) PutUserPolicy(_ context.Context, _ *iam.PutUserPolicyInput, _ ...func(*iam.Options)) (*iam.PutUserPolicyOutput, error) {
	if m.putUserPolicyErr != nil {
		return nil, m.putUserPolicyErr
	}
	return &iam.PutUserPolicyOutput{}, nil
}

func (m *adminUserIAMMock) CreateAccessKey(_ context.Context, _ *iam.CreateAccessKeyInput, _ ...func(*iam.Options)) (*iam.CreateAccessKeyOutput, error) {
	if m.createAccessKeyErr != nil {
		return nil, m.createAccessKeyErr
	}
	if m.createAccessKeyOut != nil {
		return m.createAccessKeyOut, nil
	}
	return &iam.CreateAccessKeyOutput{
		AccessKey: &iamtypes.AccessKey{
			AccessKeyId:     sdkaws.String("AKIATEST"),
			SecretAccessKey: sdkaws.String("test-secret"),
		},
	}, nil
}

func (m *adminUserIAMMock) ListAccessKeys(_ context.Context, _ *iam.ListAccessKeysInput, _ ...func(*iam.Options)) (*iam.ListAccessKeysOutput, error) {
	if m.listAccessKeysErr != nil {
		return nil, m.listAccessKeysErr
	}
	if m.listAccessKeysOut != nil {
		return m.listAccessKeysOut, nil
	}
	return &iam.ListAccessKeysOutput{}, nil
}

func (m *adminUserIAMMock) DeleteAccessKey(_ context.Context, params *iam.DeleteAccessKeyInput, _ ...func(*iam.Options)) (*iam.DeleteAccessKeyOutput, error) {
	m.deleteAccessKeyInputs = append(m.deleteAccessKeyInputs, params)
	if m.deleteAccessKeyErr != nil {
		return nil, m.deleteAccessKeyErr
	}
	return &iam.DeleteAccessKeyOutput{}, nil
}

const testRoleARN = "arn:aws:iam::123456789012:role/platform-admin"

// TestCreateAdminUserNewUser verifies that when the user does not exist,
// CreateUser, PutUserPolicy, and CreateAccessKey are all called.
func TestCreateAdminUserNewUser(t *testing.T) {
	t.Parallel()

	m := &adminUserIAMMock{}

	got, err := CreateAdminUser(context.Background(), m, "acme-admin", testRoleARN, nil)
	if err != nil {
		t.Fatalf("CreateAdminUser() unexpected error: %v", err)
	}
	if got.UserName != "acme-admin" {
		t.Errorf("UserName: want acme-admin, got %s", got.UserName)
	}
	if got.AccessKeyID != "AKIATEST" || got.SecretAccessKey != "test-secret" {
		t.Errorf("credentials: unexpected values: %+v", got)
	}
}

// TestCreateAdminUserExistingUser verifies that when the user already exists,
// CreateUser is skipped but existing keys are deleted and a new key is created.
func TestCreateAdminUserExistingUser(t *testing.T) {
	t.Parallel()

	m := &adminUserIAMMock{
		getUserOut: &iam.GetUserOutput{User: &iamtypes.User{UserName: sdkaws.String("acme-admin")}},
		listAccessKeysOut: &iam.ListAccessKeysOutput{
			AccessKeyMetadata: []iamtypes.AccessKeyMetadata{
				{AccessKeyId: sdkaws.String("AKIA_OLD1")},
				{AccessKeyId: sdkaws.String("AKIA_OLD2")},
			},
		},
	}

	got, err := CreateAdminUser(context.Background(), m, "acme-admin", testRoleARN, nil)
	if err != nil {
		t.Fatalf("CreateAdminUser() unexpected error: %v", err)
	}
	if got.UserName != "acme-admin" {
		t.Errorf("UserName: want acme-admin, got %s", got.UserName)
	}
	// Both orphaned keys must be deleted before creating a new one.
	if len(m.deleteAccessKeyInputs) != 2 {
		t.Errorf("DeleteAccessKey calls: want 2, got %d", len(m.deleteAccessKeyInputs))
	}
}

// TestCreateAdminUserPutUserPolicyError verifies that PutUserPolicy errors
// are propagated correctly.
func TestCreateAdminUserPutUserPolicyError(t *testing.T) {
	t.Parallel()

	m := &adminUserIAMMock{
		putUserPolicyErr: errors.New("policy boom"),
	}

	_, err := CreateAdminUser(context.Background(), m, "acme-admin", testRoleARN, nil)
	if err == nil {
		t.Fatal("CreateAdminUser() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "putting admin user policy") {
		t.Errorf("error should mention putting policy, got: %v", err)
	}
}

// TestCreateAdminUserCreateAccessKeyError verifies that CreateAccessKey errors
// are propagated correctly.
func TestCreateAdminUserCreateAccessKeyError(t *testing.T) {
	t.Parallel()

	m := &adminUserIAMMock{
		createAccessKeyErr: errors.New("key boom"),
	}

	_, err := CreateAdminUser(context.Background(), m, "acme-admin", testRoleARN, nil)
	if err == nil {
		t.Fatal("CreateAdminUser() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "creating access key for admin user") {
		t.Errorf("error should mention creating access key, got: %v", err)
	}
}

// TestCreateAdminUserListAccessKeysError verifies that ListAccessKeys errors
// are propagated correctly.
func TestCreateAdminUserListAccessKeysError(t *testing.T) {
	t.Parallel()

	m := &adminUserIAMMock{
		listAccessKeysErr: errors.New("list boom"),
	}

	_, err := CreateAdminUser(context.Background(), m, "acme-admin", testRoleARN, nil)
	if err == nil {
		t.Fatal("CreateAdminUser() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "listing admin user access keys") {
		t.Errorf("error should mention listing access keys, got: %v", err)
	}
}
