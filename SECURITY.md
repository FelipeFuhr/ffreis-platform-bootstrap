# Security Policy

## Supported Versions

This project is actively maintained on the default development branch and the
latest commits merged to `main`.

Older commits, unmerged branches, and private forks should be treated as
unsupported unless explicitly stated otherwise.

## Reporting a Vulnerability

Do not open a public GitHub issue for security vulnerabilities.

Preferred reporting channel:

- Use GitHub Security Advisories / Private Vulnerability Reporting for this
  repository when available.

Fallback reporting channel:

- Email: `felipefuhr7@gmail.com`

When reporting a vulnerability, include:

- A clear description of the issue
- Affected component, command, or file path
- Reproduction steps or proof of concept
- Impact assessment
- Any suggested mitigation or fix, if available

## Response Expectations

The maintainer will try to:

- Acknowledge receipt within 5 business days
- Assess severity and impact
- Work on a fix or mitigation before public disclosure
- Coordinate disclosure timing with the reporter when appropriate

## Disclosure Policy

- Please allow reasonable time for investigation and remediation before public
  disclosure.
- Public disclosure may proceed after a fix is available or after coordinated
  disclosure timing is agreed.

## Scope

This policy covers vulnerabilities in:

- The `platform-bootstrap` CLI
- Repository automation and GitHub workflows
- Supporting scripts shipped in this repository

Out of scope:

- Vulnerabilities in third-party services or infrastructure not controlled by
  this repository
- Misconfiguration in downstream deployments that do not originate from this
  codebase
