# Agent Context

**This repo:** `ffreis-platform-bootstrap` — CLI that provisions Layer 0 AWS
infrastructure: DynamoDB registry, S3 Terraform state backend, IAM platform-admin
role, and SNS/budget alerts. Must run before any other platform layer.

## Non-obvious facts

- **All operations are idempotent.** Safe to re-run after partial failures. Each step
  checks for existing resources before creating them.

- **Exit codes:** 0 = success, 1 = user error, 2 = AWS error, 3 = partial completion.
  Do not add generic catch-all exits that map to these codes incorrectly.

- **Writes output to sibling repos.** The `--org-dir` flag points to a directory where
  `platform-org` and downstream CLIs will read generated config
  (`fetched.auto.tfvars.json`, `backend.local.hcl`). These files are gitignored in
  their destination repos.

- **Logs to stderr only.** JSON in CI, human-readable in TTY. Stdout is reserved for
  machine-readable output. Never print diagnostic text to stdout.

- **Dry-run support** via `--dry-run`. All cloud API calls must be gated by this flag.

## Structure

```
cmd/platform-bootstrap/   ← Cobra CLI entry point (no business logic here)
internal/config/          ← env + flag resolution
internal/aws/             ← AWS SDK helpers (IAM, S3, DynamoDB, SNS, Budgets)
internal/bootstrap/       ← idempotent step runners
internal/logging/         ← structured logging with TTY detection
```

## Build/run

```bash
make build
./bin/platform-bootstrap init \
  --org acme --profile bootstrap \
  --root-email root@example.com \
  --org-dir ../platform-org
```

## Layer dependency

This is **Layer 0** — no other platform repo may depend on it. It produces the
foundational resources that all downstream Terraform state backends rely on.
