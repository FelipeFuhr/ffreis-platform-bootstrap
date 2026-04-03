package bootstrap

import (
	"context"
	"testing"
)

func TestBootstrapRunner_StepOrderAndNames(t *testing.T) {
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
