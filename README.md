# ffreis-platform-bootstrap

CLI tool for bootstrapping the foundational AWS multi-account platform.
Written in Go. Uses AWS SDK v2 and Cobra.

## What it does

`platform-bootstrap init` provisions Layer 0 — the resources that must
exist before any Terraform can run:

1. AWS Organizations (enabled with all features)
2. Terraform state S3 bucket (`{org}-mgmt-l1-tfstate`)
3. Terraform lock DynamoDB table (`{org}-mgmt-l1-tflock`)
4. Bootstrap state DynamoDB table (`{org}-mgmt-bootstrap-state`)
5. Bootstrap IAM user with a scoped policy
6. Access key pair written to the local `[bootstrap]` AWS named profile

All operations are idempotent. Re-running after a partial failure is safe.

## Prerequisites

- Go 1.22+
- AWS root credentials (management account) with MFA enrolled
- `~/.aws/config` writable for local profile output

## Getting started

```sh
# 1. Resolve dependencies (generates go.sum)
make tidy

# 2. Build the binary
make build

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
  --region us-east-1
```

## Configuration

All flags can be supplied as environment variables. Flags take precedence
over environment variables; environment variables take precedence over defaults.

| Flag              | Env var                   | Default     | Required for |
|-------------------|---------------------------|-------------|--------------|
| `--org`           | `PLATFORM_ORG`            | —           | all commands |
| `--profile`       | `PLATFORM_AWS_PROFILE`    | —           | all commands |
| `--region`        | `PLATFORM_REGION`         | `us-east-1` | all commands |
| `--log-level`     | `PLATFORM_LOG_LEVEL`      | `info`      | all commands |
| `--dry-run`       | `PLATFORM_DRY_RUN`        | `false`     | all commands |
| `--root-email`    | `PLATFORM_ROOT_EMAIL`     | —           | `init`       |
| `--state-region`  | `PLATFORM_STATE_REGION`   | `--region`  | `init`       |
| `--allowed-regions` | `PLATFORM_ALLOWED_REGIONS` | —        | `init`       |

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

./bin/platform-bootstrap init
```

## Project structure

```
platform-bootstrap/
├── main.go                          entry point
├── cmd/
│   ├── root.go                      root command, global flags, PersistentPreRunE
│   └── init.go                      `init` subcommand
├── internal/
│   ├── config/
│   │   ├── defaults.go              constants: defaults, env var names, naming patterns
│   │   └── config.go                Config struct, Load(), Validate()
│   ├── aws/
│   │   └── session.go               AWS credential resolution, sts:GetCallerIdentity
│   ├── bootstrap/
│   │   └── bootstrap.go             Step type, Run() orchestrator (stub)
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
make lint         # run golangci-lint (requires separate install)
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
