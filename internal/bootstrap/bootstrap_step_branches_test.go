package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/config"
)

type tempUserBootstrapIAMMock struct {
	createUserInput       *iam.CreateUserInput
	createUserErr         error
	putUserPolicyInput    *iam.PutUserPolicyInput
	putUserPolicyErr      error
	createAccessKeyInput  *iam.CreateAccessKeyInput
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

const errNotImplemented = "not implemented"

func (m *tempUserBootstrapIAMMock) GetRole(context.Context, *iam.GetRoleInput, ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
	return nil, errors.New(errNotImplemented)
}

func (m *tempUserBootstrapIAMMock) GetAccountSummary(context.Context, *iam.GetAccountSummaryInput, ...func(*iam.Options)) (*iam.GetAccountSummaryOutput, error) {
	return &iam.GetAccountSummaryOutput{}, nil
}

func (m *tempUserBootstrapIAMMock) CreateRole(context.Context, *iam.CreateRoleInput, ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	return nil, errors.New(errNotImplemented)
}

func (m *tempUserBootstrapIAMMock) UpdateAssumeRolePolicy(context.Context, *iam.UpdateAssumeRolePolicyInput, ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error) {
	return nil, errors.New(errNotImplemented)
}

func (m *tempUserBootstrapIAMMock) PutRolePolicy(context.Context, *iam.PutRolePolicyInput, ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
	return nil, errors.New(errNotImplemented)
}

func (m *tempUserBootstrapIAMMock) TagRole(context.Context, *iam.TagRoleInput, ...func(*iam.Options)) (*iam.TagRoleOutput, error) {
	return nil, errors.New(errNotImplemented)
}

func (m *tempUserBootstrapIAMMock) ListRolePolicies(context.Context, *iam.ListRolePoliciesInput, ...func(*iam.Options)) (*iam.ListRolePoliciesOutput, error) {
	return nil, errors.New(errNotImplemented)
}

func (m *tempUserBootstrapIAMMock) DeleteRolePolicy(context.Context, *iam.DeleteRolePolicyInput, ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
	return nil, errors.New(errNotImplemented)
}

func (m *tempUserBootstrapIAMMock) DeleteRole(context.Context, *iam.DeleteRoleInput, ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
	return nil, errors.New(errNotImplemented)
}

func (m *tempUserBootstrapIAMMock) GetUser(context.Context, *iam.GetUserInput, ...func(*iam.Options)) (*iam.GetUserOutput, error) {
	return nil, &iamtypes.NoSuchEntityException{}
}

func (m *tempUserBootstrapIAMMock) CreateUser(_ context.Context, params *iam.CreateUserInput, _ ...func(*iam.Options)) (*iam.CreateUserOutput, error) {
	m.createUserInput = params
	if m.createUserErr != nil {
		return nil, m.createUserErr
	}
	return &iam.CreateUserOutput{User: &iamtypes.User{UserName: params.UserName}}, nil
}

func (m *tempUserBootstrapIAMMock) PutUserPolicy(_ context.Context, params *iam.PutUserPolicyInput, _ ...func(*iam.Options)) (*iam.PutUserPolicyOutput, error) {
	m.putUserPolicyInput = params
	if m.putUserPolicyErr != nil {
		return nil, m.putUserPolicyErr
	}
	return &iam.PutUserPolicyOutput{}, nil
}

func (m *tempUserBootstrapIAMMock) CreateAccessKey(_ context.Context, params *iam.CreateAccessKeyInput, _ ...func(*iam.Options)) (*iam.CreateAccessKeyOutput, error) {
	m.createAccessKeyInput = params
	if m.createAccessKeyErr != nil {
		return nil, m.createAccessKeyErr
	}
	return &iam.CreateAccessKeyOutput{
		AccessKey: &iamtypes.AccessKey{
			AccessKeyId:     sdkaws.String("AKIATEST"),
			SecretAccessKey: sdkaws.String("secret"),
		},
	}, nil
}

func (m *tempUserBootstrapIAMMock) ListAccessKeys(context.Context, *iam.ListAccessKeysInput, ...func(*iam.Options)) (*iam.ListAccessKeysOutput, error) {
	if m.listAccessKeysErr != nil {
		return nil, m.listAccessKeysErr
	}
	if m.listAccessKeysOut != nil {
		return m.listAccessKeysOut, nil
	}
	return &iam.ListAccessKeysOutput{}, nil
}

func (m *tempUserBootstrapIAMMock) DeleteAccessKey(_ context.Context, params *iam.DeleteAccessKeyInput, _ ...func(*iam.Options)) (*iam.DeleteAccessKeyOutput, error) {
	m.deleteAccessKeyInputs = append(m.deleteAccessKeyInputs, params)
	if m.deleteAccessKeyErr != nil {
		return nil, m.deleteAccessKeyErr
	}
	return &iam.DeleteAccessKeyOutput{}, nil
}

func (m *tempUserBootstrapIAMMock) DeleteUserPolicy(_ context.Context, params *iam.DeleteUserPolicyInput, _ ...func(*iam.Options)) (*iam.DeleteUserPolicyOutput, error) {
	m.deleteUserPolicyInput = params
	if m.deleteUserPolicyErr != nil {
		return nil, m.deleteUserPolicyErr
	}
	return &iam.DeleteUserPolicyOutput{}, nil
}

func (m *tempUserBootstrapIAMMock) DeleteUser(_ context.Context, params *iam.DeleteUserInput, _ ...func(*iam.Options)) (*iam.DeleteUserOutput, error) {
	m.deleteUserInput = params
	if m.deleteUserErr != nil {
		return nil, m.deleteUserErr
	}
	return &iam.DeleteUserOutput{}, nil
}

func TestBootstrapStepDefsCreateTempUserRootPath(t *testing.T) {
	t.Parallel()

	cfg := minimalConfig()
	iamClient := &tempUserBootstrapIAMMock{}
	r := newBootstrapRunner(context.Background(), cfg, &platformaws.Clients{
		IAM:       iamClient,
		AccountID: "123456789012",
		CallerARN: "arn:aws:iam::123456789012:root",
		Region:    testRegion,
	})

	step := findBootstrapStepDef(t, cfg, "create-temp-user")
	if err := step.run(context.Background(), r); err != nil {
		t.Fatalf("create-temp-user step: %v", err)
	}
	if r.tempUser == nil {
		t.Fatal("expected temp user to be stored on runner")
	}
	if r.rootIAM != iamClient {
		t.Fatal("expected root IAM client to be preserved for cleanup")
	}
}

func TestBootstrapStepDefsAssumeAdminRoleSkipsWithoutAssumer(t *testing.T) {
	t.Parallel()

	cfg := minimalConfig()
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		CallerARN: "arn:aws:iam::123456789012:user/bootstrap",
		Region:    testRegion,
	}
	r := newBootstrapRunner(context.Background(), cfg, clients)

	step := findBootstrapStepDef(t, cfg, "assume-admin-role")
	if err := step.run(context.Background(), r); err != nil {
		t.Fatalf("assume-admin-role step: %v", err)
	}
	if r.c != clients {
		t.Fatal("expected clients bundle to remain unchanged when assumption is skipped")
	}
}

func TestBootstrapStepDefsPlatformBudgetRequiresTopic(t *testing.T) {
	t.Parallel()

	cfg := minimalConfig()
	r := newBootstrapRunner(context.Background(), cfg, &platformaws.Clients{
		AccountID: "123456789012",
		CallerARN: testCallerARN,
		Region:    testRegion,
	})

	step := findBootstrapStepDef(t, cfg, "platform-budget")
	err := step.run(context.Background(), r)
	if err == nil {
		t.Fatal("expected platform-budget to fail when topic is missing")
	}
	if !strings.Contains(err.Error(), "requires platform-events-topic") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBootstrapStepDefsDeleteTempUserSkipsWhenNil(t *testing.T) {
	t.Parallel()

	cfg := minimalConfig()
	r := newBootstrapRunner(context.Background(), cfg, &platformaws.Clients{
		CallerARN: testCallerARN,
		Region:    testRegion,
	})

	step := findBootstrapStepDef(t, cfg, "delete-temp-user")
	if err := step.run(context.Background(), r); err != nil {
		t.Fatalf("delete-temp-user step: %v", err)
	}
}

func TestBootstrapStepDefsDeleteTempUserUsesCurrentIAMWhenRootClientMissing(t *testing.T) {
	t.Parallel()

	cfg := minimalConfig()
	iamClient := &tempUserBootstrapIAMMock{}
	r := newBootstrapRunner(context.Background(), cfg, &platformaws.Clients{
		IAM:       iamClient,
		CallerARN: testCallerARN,
		Region:    testRegion,
	})
	r.tempUser = &platformaws.TempUser{UserName: platformaws.TempBootstrapUserName}

	step := findBootstrapStepDef(t, cfg, "delete-temp-user")
	if err := step.run(context.Background(), r); err != nil {
		t.Fatalf("delete-temp-user step: %v", err)
	}
	if r.tempUser != nil {
		t.Fatal("expected temp user to be cleared after cleanup")
	}
	if iamClient.deleteUserInput == nil {
		t.Fatal("expected IAM delete user call")
	}
}

func TestRunAccountConfigPropagatesWriteErrors(t *testing.T) {
	t.Parallel()

	cfg := minimalConfig()
	cfg.Accounts = map[string]string{"dev": "dev@example.com"}

	db := &fakeDynamoDB{putErr: errors.New("dynamo down")}
	r := newBootstrapRunner(context.Background(), cfg, &platformaws.Clients{
		DynamoDB:  db,
		CallerARN: testCallerARN,
	})

	err := runAccountConfig(context.Background(), r, cfg)
	if err == nil {
		t.Fatal("expected runAccountConfig to fail when registry write fails")
	}
	if !strings.Contains(err.Error(), "writing account config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func findBootstrapStepDef(t *testing.T, cfg *config.Config, name string) bootstrapStepDef {
	t.Helper()

	for _, def := range bootstrapStepDefs(cfg) {
		if def.name == name {
			return def
		}
	}
	t.Fatalf("step %q not found", name)
	return bootstrapStepDef{}
}
