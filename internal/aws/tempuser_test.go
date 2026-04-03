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

const (
	errCreateTempUserUnexpected = "CreateTempBootstrapUser() unexpected error: %v"
	errCreateTempUserExpected   = "CreateTempBootstrapUser() expected error, got nil"
)

type tempUserIAMMock struct {
	mockIAM
	getUserOut            *iam.GetUserOutput
	getUserErr            error
	createUserInput       *iam.CreateUserInput
	createUserErr         error
	putUserPolicyInput    *iam.PutUserPolicyInput
	putUserPolicyErr      error
	createAccessKeyInput  *iam.CreateAccessKeyInput
	createAccessKeyOut    *iam.CreateAccessKeyOutput
	createAccessKeyErr    error
	listAccessKeysOut     *iam.ListAccessKeysOutput
	listAccessKeysErr     error
	deleteAccessKeyInputs []*iam.DeleteAccessKeyInput
	deleteAccessKeyErr    error
	deleteUserPolicyInput *iam.DeleteUserPolicyInput
	deleteUserPolicyErr   error
	deleteUserInput       *iam.DeleteUserInput
	deleteUserErr         error
}

func (m *tempUserIAMMock) GetUser(_ context.Context, _ *iam.GetUserInput, _ ...func(*iam.Options)) (*iam.GetUserOutput, error) {
	if m.getUserErr != nil {
		return nil, m.getUserErr
	}
	if m.getUserOut != nil {
		return m.getUserOut, nil
	}
	return nil, &iamtypes.NoSuchEntityException{}
}

func (m *tempUserIAMMock) CreateUser(_ context.Context, params *iam.CreateUserInput, _ ...func(*iam.Options)) (*iam.CreateUserOutput, error) {
	m.createUserInput = params
	if m.createUserErr != nil {
		return nil, m.createUserErr
	}
	return &iam.CreateUserOutput{User: &iamtypes.User{UserName: params.UserName}}, nil
}

func (m *tempUserIAMMock) PutUserPolicy(_ context.Context, params *iam.PutUserPolicyInput, _ ...func(*iam.Options)) (*iam.PutUserPolicyOutput, error) {
	m.putUserPolicyInput = params
	if m.putUserPolicyErr != nil {
		return nil, m.putUserPolicyErr
	}
	return &iam.PutUserPolicyOutput{}, nil
}

func (m *tempUserIAMMock) CreateAccessKey(_ context.Context, params *iam.CreateAccessKeyInput, _ ...func(*iam.Options)) (*iam.CreateAccessKeyOutput, error) {
	m.createAccessKeyInput = params
	if m.createAccessKeyErr != nil {
		return nil, m.createAccessKeyErr
	}
	if m.createAccessKeyOut != nil {
		return m.createAccessKeyOut, nil
	}
	return &iam.CreateAccessKeyOutput{
		AccessKey: &iamtypes.AccessKey{
			AccessKeyId:     sdkaws.String("AKIATEST"),
			SecretAccessKey: sdkaws.String("secret"),
		},
	}, nil
}

func (m *tempUserIAMMock) ListAccessKeys(_ context.Context, _ *iam.ListAccessKeysInput, _ ...func(*iam.Options)) (*iam.ListAccessKeysOutput, error) {
	if m.listAccessKeysErr != nil {
		return nil, m.listAccessKeysErr
	}
	if m.listAccessKeysOut != nil {
		return m.listAccessKeysOut, nil
	}
	return &iam.ListAccessKeysOutput{}, nil
}

func (m *tempUserIAMMock) DeleteAccessKey(_ context.Context, params *iam.DeleteAccessKeyInput, _ ...func(*iam.Options)) (*iam.DeleteAccessKeyOutput, error) {
	m.deleteAccessKeyInputs = append(m.deleteAccessKeyInputs, params)
	if m.deleteAccessKeyErr != nil {
		return nil, m.deleteAccessKeyErr
	}
	return &iam.DeleteAccessKeyOutput{}, nil
}

func (m *tempUserIAMMock) DeleteUserPolicy(_ context.Context, params *iam.DeleteUserPolicyInput, _ ...func(*iam.Options)) (*iam.DeleteUserPolicyOutput, error) {
	m.deleteUserPolicyInput = params
	if m.deleteUserPolicyErr != nil {
		return nil, m.deleteUserPolicyErr
	}
	return &iam.DeleteUserPolicyOutput{}, nil
}

func (m *tempUserIAMMock) DeleteUser(_ context.Context, params *iam.DeleteUserInput, _ ...func(*iam.Options)) (*iam.DeleteUserOutput, error) {
	m.deleteUserInput = params
	if m.deleteUserErr != nil {
		return nil, m.deleteUserErr
	}
	return &iam.DeleteUserOutput{}, nil
}

func TestCreateTempBootstrapUserCreatesUserPolicyAndKey(t *testing.T) {
	t.Parallel()

	m := &tempUserIAMMock{
		getUserErr: &iamtypes.NoSuchEntityException{},
		createAccessKeyOut: &iam.CreateAccessKeyOutput{
			AccessKey: &iamtypes.AccessKey{
				AccessKeyId:     sdkaws.String("AKIATEST"),
				SecretAccessKey: sdkaws.String("secret"),
			},
		},
	}

	got, err := CreateTempBootstrapUser(context.Background(), m, testPlatformAdminRoleARN, map[string]string{
		"ManagedBy": "platform-bootstrap",
		"Project":   "platform",
	})
	if err != nil {
		t.Fatalf(errCreateTempUserUnexpected, err)
	}
	if got.UserName != TempBootstrapUserName || got.AccessKeyID != "AKIATEST" || got.SecretAccessKey != "secret" {
		t.Fatalf("CreateTempBootstrapUser() returned unexpected user: %+v", got)
	}
	if m.createUserInput == nil {
		t.Fatal("expected CreateUser to be called")
	}
	if m.putUserPolicyInput == nil {
		t.Fatal("expected PutUserPolicy to be called")
	}
	policyDoc := sdkaws.ToString(m.putUserPolicyInput.PolicyDocument)
	if !strings.Contains(policyDoc, "sts:AssumeRole") || !strings.Contains(policyDoc, "platform-admin") {
		t.Fatalf("policy document missing assume-role details: %s", policyDoc)
	}
	if len(m.createUserInput.Tags) != 2 {
		t.Fatalf("CreateUser tags: want 2, got %d", len(m.createUserInput.Tags))
	}
}

func TestCreateTempBootstrapUserReusesExistingUser(t *testing.T) {
	t.Parallel()

	m := &tempUserIAMMock{
		getUserOut: &iam.GetUserOutput{User: &iamtypes.User{UserName: sdkaws.String(TempBootstrapUserName)}},
	}

	_, err := CreateTempBootstrapUser(context.Background(), m, testPlatformAdminRoleARN, nil)
	if err != nil {
		t.Fatalf(errCreateTempUserUnexpected, err)
	}
	if m.createUserInput != nil {
		t.Fatal("CreateUser should not be called when temp user already exists")
	}
	if m.putUserPolicyInput == nil || m.createAccessKeyInput == nil {
		t.Fatal("expected policy and access key to still be refreshed")
	}
}

func TestCreateTempBootstrapUserCreateAccessKeyError(t *testing.T) {
	t.Parallel()

	m := &tempUserIAMMock{
		getUserErr:         &iamtypes.NoSuchEntityException{},
		createAccessKeyErr: errors.New("key boom"),
	}

	_, err := CreateTempBootstrapUser(context.Background(), m, testPlatformAdminRoleARN, nil)
	if err == nil {
		t.Fatal(errCreateTempUserExpected)
	}
	if !strings.Contains(err.Error(), "creating access key for temp user") {
		t.Fatalf("CreateTempBootstrapUser() should wrap key error, got: %v", err)
	}
}

func TestEnsureTempUserExistsConcurrentCreateAllowed(t *testing.T) {
	t.Parallel()

	m := &tempUserIAMMock{
		getUserErr:    &iamtypes.NoSuchEntityException{},
		createUserErr: &iamtypes.EntityAlreadyExistsException{},
	}

	if err := ensureTempUserExists(context.Background(), m, nil); err != nil {
		t.Fatalf("ensureTempUserExists() unexpected error: %v", err)
	}
}

func TestEnsureTempUserExistsUnexpectedLookupError(t *testing.T) {
	t.Parallel()

	m := &tempUserIAMMock{getUserErr: errors.New("iam boom")}
	err := ensureTempUserExists(context.Background(), m, nil)
	if err == nil {
		t.Fatal("ensureTempUserExists() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "checking temp user") {
		t.Fatalf("ensureTempUserExists() should wrap lookup error, got: %v", err)
	}
}

func TestDeleteTempBootstrapUserDeletesAllKeysAndIgnoresMissingCleanup(t *testing.T) {
	t.Parallel()

	m := &tempUserIAMMock{
		listAccessKeysOut: &iam.ListAccessKeysOutput{
			AccessKeyMetadata: []iamtypes.AccessKeyMetadata{
				{AccessKeyId: sdkaws.String("AKIA1")},
				{AccessKeyId: sdkaws.String("AKIA2")},
			},
		},
		deleteUserPolicyErr: &iamtypes.NoSuchEntityException{},
		deleteUserErr:       &iamtypes.NoSuchEntityException{},
	}

	err := DeleteTempBootstrapUser(context.Background(), m, TempUser{UserName: TempBootstrapUserName})
	if err != nil {
		t.Fatalf("DeleteTempBootstrapUser() unexpected error: %v", err)
	}
	if len(m.deleteAccessKeyInputs) != 2 {
		t.Fatalf("DeleteAccessKey calls: want 2, got %d", len(m.deleteAccessKeyInputs))
	}
	if m.deleteUserPolicyInput == nil || m.deleteUserInput == nil {
		t.Fatal("expected policy and user deletes to be attempted")
	}
}

func TestDeleteTempBootstrapUserListKeysError(t *testing.T) {
	t.Parallel()

	m := &tempUserIAMMock{listAccessKeysErr: errors.New("list boom")}
	err := DeleteTempBootstrapUser(context.Background(), m, TempUser{UserName: TempBootstrapUserName})
	if err == nil {
		t.Fatal("DeleteTempBootstrapUser() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "listing temp user access keys") {
		t.Fatalf("DeleteTempBootstrapUser() should wrap list error, got: %v", err)
	}
}

func TestIsTempUserPropagationError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "access denied", err: errors.New("AccessDenied: nope"), want: true},
		{name: "invalid token", err: errors.New("InvalidClientTokenId: nope"), want: true},
		{name: "other", err: errors.New("Throttling"), want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTempUserPropagationError(tc.err); got != tc.want {
				t.Fatalf("isTempUserPropagationError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestCreateTempBootstrapUserDeletesOrphanedKeysBeforeCreating(t *testing.T) {
	t.Parallel()

	// Simulate a user that already exists with 2 orphaned keys from a previous run.
	m := &tempUserIAMMock{
		getUserOut: &iam.GetUserOutput{User: &iamtypes.User{UserName: sdkaws.String(TempBootstrapUserName)}},
		listAccessKeysOut: &iam.ListAccessKeysOutput{
			AccessKeyMetadata: []iamtypes.AccessKeyMetadata{
				{AccessKeyId: sdkaws.String("AKIA_ORPHAN1")},
				{AccessKeyId: sdkaws.String("AKIA_ORPHAN2")},
			},
		},
	}

	got, err := CreateTempBootstrapUser(context.Background(), m, testPlatformAdminRoleARN, nil)
	if err != nil {
		t.Fatalf("CreateTempBootstrapUser() unexpected error: %v", err)
	}
	if got.UserName != TempBootstrapUserName {
		t.Fatalf("CreateTempBootstrapUser() returned unexpected user: %+v", got)
	}
	if len(m.deleteAccessKeyInputs) != 2 {
		t.Fatalf("expected 2 orphaned keys to be deleted, got %d", len(m.deleteAccessKeyInputs))
	}
	if m.createAccessKeyInput == nil {
		t.Fatal("expected a new access key to be created")
	}
}

func TestCreateTempBootstrapUserListAccessKeysError(t *testing.T) {
	t.Parallel()

	m := &tempUserIAMMock{
		getUserErr:        &iamtypes.NoSuchEntityException{},
		listAccessKeysErr: errors.New("iam list keys boom"),
	}

	_, err := CreateTempBootstrapUser(context.Background(), m, testPlatformAdminRoleARN, nil)
	if err == nil {
		t.Fatal("CreateTempBootstrapUser() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "listing temp user access keys") {
		t.Fatalf("CreateTempBootstrapUser() should wrap list keys error, got: %v", err)
	}
}

func TestCreateTempBootstrapUserDeleteOrphanedKeyError(t *testing.T) {
	t.Parallel()

	m := &tempUserIAMMock{
		getUserErr: &iamtypes.NoSuchEntityException{},
		listAccessKeysOut: &iam.ListAccessKeysOutput{
			AccessKeyMetadata: []iamtypes.AccessKeyMetadata{
				{AccessKeyId: sdkaws.String("AKIA_ORPHAN1")},
			},
		},
		deleteAccessKeyErr: errors.New("delete key boom"),
	}

	_, err := CreateTempBootstrapUser(context.Background(), m, testPlatformAdminRoleARN, nil)
	if err == nil {
		t.Fatal("CreateTempBootstrapUser() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "deleting orphaned temp user access key") {
		t.Fatalf("CreateTempBootstrapUser() should wrap delete key error, got: %v", err)
	}
}

func TestIsNoSuchEntity(t *testing.T) {
	t.Parallel()

	if !isNoSuchEntity(&iamtypes.NoSuchEntityException{}) {
		t.Fatal("expected NoSuchEntityException to match")
	}
	if isNoSuchEntity(errors.New("boom")) {
		t.Fatal("did not expect generic error to match")
	}
}
