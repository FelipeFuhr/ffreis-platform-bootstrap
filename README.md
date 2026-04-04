# ffreis-platform-bootstrap

CLI tool for bootstrapping the foundational AWS multi-account platform.
Written in Go. Uses AWS SDK v2 and Cobra.

## What it does

`platform-bootstrap init` provisions Layer 0 — the resources that must
exist before any Terraform can run:

1. Bootstrap registry DynamoDB table (`{org}-bootstrap-registry`)
2. Terraform state S3 bucket (`{org}-tf-state-root`)
3. Terraform lock DynamoDB table (`{org}-tf-locks-root`)
4. Platform admin IAM role (`platform-admin`)
5. Platform events SNS topic (`{org}-platform-events`) + budget publish policy
6. Monthly AWS Budget with SNS alerts (`{org}-platform-monthly-budget`)

All operations are idempotent. Re-running after a partial failure is safe.

## Prerequisites

- Go 1.25+
- AWS credentials for an **administrator principal in the management account**
  (root is not required, but the principal must be allowed to call IAM, S3,
  DynamoDB, Budgets, and SNS APIs)
- An AWS CLI/SDK credential source (profile, env vars, or SSO cached login)

## Getting started

```sh
# 1. Resolve dependencies (generates go.sum)
make tidy

# 2. Build the binary
make build

# Or build directly from the real entrypoint package
go build -o ./bin/platform-bootstrap ./cmd/platform-bootstrap

# 2a. Optional: diagnose credentials & permissions (read-only)
./bin/platform-bootstrap doctor --org acme --profile bootstrap --region us-east-1

# 2a. If you use AWS SSO / IAM Identity Center (AWS CLI v2)
# aws configure sso
# aws sso login --profile <your-profile>

# 3. Dry-run to confirm what will be created
make run-init-dry ORG=acme PROFILE=bootstrap ROOT_EMAIL=root@acme.example.com

# 4. Execute for real
make run-init ORG=acme PROFILE=bootstrap ROOT_EMAIL=root@acme.example.com
```

Or invoke the binary directly:

```sh
./bin/platform-bootstrap init \
  --org acme \
  --profile bootstrap \
  --root-email root@acme.example.com \
  --region us-east-1 \
  --org-dir ../your-platform-org-repo
```

`--org-dir` is optional, but recommended when the sibling platform org
Terraform repo is checked out next to this one. When set, bootstrap writes the
fetched Terraform inputs that the next layer needs so you can move directly to
apply under `terraform/envs/` and `terraform/stack/`.

## Configuration

All flags can be supplied as environment variables. Flags take precedence
over environment variables; environment variables take precedence over defaults.

| Flag              | Env var                   | Default     | Required for |
|-------------------|---------------------------|-------------|--------------|
| `--org`           | `PLATFORM_ORG`            | —           | all commands |
| `--profile`       | `PLATFORM_AWS_PROFILE` (or `AWS_PROFILE`) | — | all commands |
| `--region`        | `PLATFORM_REGION`         | `us-east-1` | all commands |
| `--log-level`     | `PLATFORM_LOG_LEVEL`      | `info`      | all commands |
| `--dry-run`       | `PLATFORM_DRY_RUN`        | `false`     | all commands |
| `--root-email`    | `PLATFORM_ROOT_EMAIL`     | —           | `init`       |
| `--admin-email`   | `PLATFORM_ADMIN_EMAIL`    | —           | `init`       |
| `--state-region`  | `PLATFORM_STATE_REGION`   | `--region`  | `init`       |
| `--allowed-regions` | `PLATFORM_ALLOWED_REGIONS` | —        | `init`       |
| `--org-dir`       | —                         | —           | `init`       |

`--allowed-regions` is comma-separated both in the flag and env var:

```sh
--allowed-regions us-east-1,eu-west-1
PLATFORM_ALLOWED_REGIONS=us-east-1,eu-west-1
```

## Logging

Logs are written to **stderr**. Stdout is reserved for machine-readable output.

- Interactive terminal → human-readable text format
- Non-TTY / CI → JSON (structured, machine-parseable)
- `--log-level debug` → includes source file/line in every log line

## CI usage

In CI, supply credentials via environment variables instead of a named profile:

```sh
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...
export AWS_SESSION_TOKEN=...        # if using temporary credentials
export PLATFORM_ORG=acme
export PLATFORM_REGION=us-east-1
export PLATFORM_ROOT_EMAIL=root@acme.example.com

./bin/platform-bootstrap init --org-dir ../your-platform-org-repo
```

## Project structure

```
platform-bootstrap/
├── cmd/
│   ├── platform-bootstrap/
│   │   └── main.go                  entry point
│   ├── root.go                      root command, global flags, PersistentPreRunE
│   ├── init.go                      `init` subcommand
│   ├── fetch.go                     write fetched org-layer config files
│   ├── audit.go                     compare registry vs AWS reality
│   └── nuke.go                      delete Layer 0 resources
├── internal/
│   ├── config/
│   │   ├── defaults.go              constants: defaults, env var names, naming patterns
│   │   └── config.go                Config struct, Load(), Validate()
│   ├── aws/
│   │   ├── session.go               AWS credential resolution, sts:GetCallerIdentity
│   │   ├── registry.go              bootstrap registry reads/writes
│   │   └── *.go                     AWS resource creation helpers
│   ├── bootstrap/
│   │   ├── bootstrap.go             ordered bootstrap steps and Run() orchestration
│   │   ├── nuke.go                  reverse-order cleanup orchestration
│   │   └── step_runner.go           shared step execution/reporting
│   └── logging/
│       ├── logger.go                slog logger construction, IsTTY()
│       └── context.go               WithLogger / FromContext
├── Makefile
├── README.md
└── go.mod
```

## Development

```sh
make fmt          # format source
make test         # run tests
make lint         # run golangci-lint v2 (install: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.4.0)
make build        # compile binary to ./bin/
make clean        # remove ./bin/
```

## Exit codes

| Code | Meaning                                      |
|------|----------------------------------------------|
| `0`  | Success                                      |
| `1`  | User error (bad flags, invalid config)       |
| `2`  | AWS error (credentials, API failure)         |
| `3`  | Partial completion (some steps ran, one failed) |
