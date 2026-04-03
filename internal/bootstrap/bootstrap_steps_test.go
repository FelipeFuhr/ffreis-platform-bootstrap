package bootstrap

import (
	"context"
	"strings"
	"testing"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/config"
)

func TestBootstrapRunnerStepOrderAndNames(t *testing.T) {
	cfg := minimalConfig()

	r := newBootstrapRunner(context.Background(), cfg, nil)
	steps := r.steps()

	want := []string{
		"platform-admin-role",
		"create-temp-user",
		"assume-admin-role",
		"registry-table",
		"register-admin-role",
		"account-config",
		"state-bucket",
		"lock-table",
		"platform-events-topic",
		"platform-events-policy",
		"platform-budget",
		"delete-temp-user",
	}

	if len(steps) != len(want) {
		t.Fatalf("step count: want %d, got %d", len(want), len(steps))
	}

	for i, w := range want {
		if steps[i].name != w {
			t.Fatalf("step %d name: want %q, got %q", i, w, steps[i].name)
		}
	}
}

func TestBootstrapStepDefHelpersMetadata(t *testing.T) {
	cfg := minimalConfig()

	cases := []struct {
		name         string
		def          bootstrapStepDef
		resourceType ResourceType
		resourceName string
		descContains string
	}{
		{name: "platform-admin-role", def: platformAdminRoleStepDef(), descContains: config.RoleNamePlatformAdmin},
		{name: "create-temp-user", def: createTempUserStepDef(), descContains: platformaws.TempBootstrapUserName},
		{name: "assume-admin-role", def: assumeAdminRoleStepDef(), descContains: config.RoleNamePlatformAdmin},
		{name: "registry-table", def: registryTableStepDef(cfg), resourceType: ResourceTypeDynamoDBTable, resourceName: cfg.RegistryTableName(), descContains: cfg.RegistryTableName()},
		{name: "register-admin-role", def: registerAdminRoleStepDef(), resourceType: ResourceTypeIAMRole, resourceName: config.RoleNamePlatformAdmin, descContains: config.RoleNamePlatformAdmin},
		{name: stepAccountConfig, def: accountConfigStepDef(cfg), descContains: cfg.RegistryTableName()},
		{name: "state-bucket", def: stateBucketStepDef(cfg), resourceType: ResourceTypeS3Bucket, resourceName: cfg.StateBucketName(), descContains: cfg.StateBucketName()},
		{name: "lock-table", def: lockTableStepDef(cfg), resourceType: ResourceTypeDynamoDBTable, resourceName: cfg.LockTableName(), descContains: cfg.LockTableName()},
		{name: "platform-events-topic", def: platformEventsTopicStepDef(cfg), resourceType: ResourceTypeSNSTopic, resourceName: cfg.EventsTopicName(), descContains: cfg.EventsTopicName()},
		{name: "platform-events-policy", def: platformEventsPolicyStepDef(cfg), descContains: cfg.EventsTopicName()},
		{name: "platform-budget", def: platformBudgetStepDef(cfg), resourceType: ResourceTypeAWSBudget, resourceName: cfg.BudgetName(), descContains: cfg.BudgetName()},
		{name: "delete-temp-user", def: deleteTempUserStepDef(), descContains: platformaws.TempBootstrapUserName},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertStepDefMetadata(t, tc.def, tc.name, tc.resourceType, tc.resourceName, tc.descContains)
		})
	}
}

func assertStepDefMetadata(t *testing.T, def bootstrapStepDef, wantName string, wantResourceType ResourceType, wantResourceName, descContains string) {
	t.Helper()

	if def.name != wantName {
		t.Fatalf("step name: got %q, want %q", def.name, wantName)
	}
	if def.resourceType != wantResourceType {
		t.Fatalf("resource type: got %q, want %q", def.resourceType, wantResourceType)
	}
	if def.resourceName != wantResourceName {
		t.Fatalf("resource name: got %q, want %q", def.resourceName, wantResourceName)
	}
	if def.run == nil {
		t.Fatal("expected step run func")
	}
	if !strings.Contains(def.desc, descContains) {
		t.Fatalf("desc %q does not contain %q", def.desc, descContains)
	}
}
