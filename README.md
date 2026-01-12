# Unstuck

[![CI](https://github.com/sozercan/unstuck/actions/workflows/ci.yml/badge.svg)](https://github.com/sozercan/unstuck/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/sozercan/unstuck)](https://goreportcard.com/report/github.com/sozercan/unstuck)

A CLI tool to diagnose and remediate Kubernetes resources stuck in `Terminating` state due to unsatisfiable finalizers, missing controllers, deleted CRDs, or blocking webhooks.

## Features

- **Diagnose** stuck namespaces, CRDs, and resources
- **Identify** blocking finalizers, discovery failures, and webhook issues
- **Generate** remediation plans with escalating risk levels
- **Execute** fixes with dry-run support and audit logging

## Installation

### Homebrew

```bash
brew install sozercan/tap/unstuck
```

### Krew (kubectl plugin)

```bash
kubectl krew install unstuck
```

### Binary Download

Download the latest release from [GitHub Releases](https://github.com/sozercan/unstuck/releases).

### From Source

```bash
go install github.com/sozercan/unstuck/cmd/unstuck@latest
```

## Quick Start

### Diagnose a Stuck Namespace

```bash
unstuck diagnose namespace cert-manager
```

Example output:
```
DIAGNOSIS: Namespace "cert-manager"
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Status:         Terminating (since 2h ago)
Root Cause:     CR instances stuck with unsatisfied finalizers

BLOCKERS (3 found)
┌────┬─────────────────────────────────────┬────────────────────────────────┬──────────┐
│ #  │ Resource                            │ Finalizer                      │ Status   │
├────┼─────────────────────────────────────┼────────────────────────────────┼──────────┤
│ 1  │ Certificate/my-cert                 │ cert-manager.io/finalizer      │ Stuck    │
│ 2  │ Certificate/another-cert            │ cert-manager.io/finalizer      │ Stuck    │
│ 3  │ Issuer/letsencrypt-prod             │ cert-manager.io/finalizer      │ Stuck    │
└────┴─────────────────────────────────────┴────────────────────────────────┴──────────┘

RECOMMENDATION
3 resources are stuck in Terminating. Use `unstuck plan namespace cert-manager` to generate remediation steps.
```

### Diagnose a Stuck CRD

```bash
unstuck diagnose crd certificates.cert-manager.io
```

### Diagnose a Specific Resource

```bash
unstuck diagnose certificate my-cert -n cert-manager
```

### JSON Output (for automation)

```bash
unstuck diagnose namespace cert-manager -o json | jq '.blockers | length'
```

### Generate a Remediation Plan

```bash
unstuck plan namespace cert-manager
```

### Dry-Run (Preview Changes)

```bash
unstuck apply namespace cert-manager --dry-run
```

### Execute Remediation

```bash
# Execute with confirmation prompts
unstuck apply namespace cert-manager

# Skip prompts (for automation)
unstuck apply namespace cert-manager --yes
```

## Commands

### `diagnose`

Analyze a stuck Kubernetes resource (read-only).

```bash
unstuck diagnose <type> <name> [flags]
```

**Supported types:**
- `namespace` / `ns` - Diagnose a namespace
- `crd` - Diagnose a CustomResourceDefinition
- `<resource>` - Diagnose any resource type (e.g., `certificate`, `pod`)

**Flags:**
- `-n, --namespace` - Namespace for resource targets
- `-o, --output` - Output format: `text`, `json` (auto-detects TTY)
- `-v, --verbose` - Verbose output with conditions

### `plan`

Generate a remediation plan for a stuck resource.

```bash
unstuck plan <type> <name> [flags]
```

**Flags:**
- `--max-escalation` - Maximum escalation level (0-4, default: 2)
- `--allow-force` - Allow Level 3-4 actions in the plan

### `apply`

Execute the remediation plan.

```bash
unstuck apply <type> <name> [flags]
```

**Flags:**
- `--dry-run` - Show what would be done without making changes
- `-y, --yes` - Skip confirmation prompts
- `--continue-on-error` - Continue even if some actions fail
- `--max-escalation` - Maximum escalation level (0-4, default: 2)
- `--allow-force` - Allow Level 3-4 actions

### Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--kubeconfig` | `~/.kube/config` | Path to kubeconfig |
| `--context` | current | Kubernetes context to use |
| `--timeout` | `5m` | Overall operation timeout |

## Escalation Levels

Unstuck uses escalation levels to manage risk:

| Level | Name | Risk | Description |
|-------|------|------|-------------|
| 0 | Informational | None | Diagnosis only |
| 1 | Clean Deletion | Low | Retry with controller |
| 2 | Finalizer Removal | Medium | Remove finalizers from resources |
| 3 | CRD Finalizer | High | Remove CRD cleanup finalizer |
| 4 | Force Finalize | Critical | Force-finalize namespace |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success / Resource is healthy |
| 1 | Error / Resource not found |
| 2 | Partial success |

## Development

### Prerequisites

- Go 1.25+
- kubectl configured with cluster access
- kind (for integration tests)

### Build

```bash
make build
```

### Test

```bash
# Unit tests
make test-unit

# Integration tests (requires kind cluster)
make test-integration
```

### Lint

```bash
make lint
```

## License

MIT
