# Unstuck User Guide

A CLI tool to diagnose and remediate Kubernetes resources stuck in `Terminating` state.

## Table of Contents

- [Quick Start](#quick-start)
- [Commands](#commands)
  - [diagnose](#diagnose)
  - [plan](#plan)
  - [apply](#apply)
- [Common Scenarios](#common-scenarios)
- [Understanding Output](#understanding-output)
- [Flags Reference](#flags-reference)
- [Troubleshooting](#troubleshooting)

---

## Quick Start

```bash
# Diagnose why a namespace is stuck
unstuck diagnose namespace my-namespace

# Generate a remediation plan
unstuck plan namespace my-namespace

# Preview what would be done (dry-run)
unstuck apply namespace my-namespace --dry-run

# Execute the remediation
unstuck apply namespace my-namespace
```

---

## Commands

### diagnose

Analyze stuck resources and identify the root cause. This is a **read-only** operation.

```bash
# Diagnose a stuck namespace
unstuck diagnose namespace <name>

# Diagnose a stuck CRD
unstuck diagnose crd <name>

# Diagnose a specific resource
unstuck diagnose <kind> <name> -n <namespace>
```

**Examples:**

```bash
# Namespace stuck after uninstalling cert-manager
unstuck diagnose namespace cert-manager

# CRD stuck in terminating
unstuck diagnose crd certificates.cert-manager.io

# Specific certificate resource
unstuck diagnose certificate my-cert -n default
```

**Output includes:**
- Current status (Terminating, Active)
- How long it's been stuck
- Root cause analysis
- List of blocking resources with finalizers
- Controller status
- Webhook interference detection
- Recommended next steps

### plan

Generate a remediation plan with ordered actions.

```bash
unstuck plan namespace <name> [flags]
```

**Flags:**
- `--max-escalation`: Maximum escalation level (0-4, default: 2)
- `--allow-force`: Allow Level 3-4 actions in the plan

**Examples:**

```bash
# Default plan (max Level 2)
unstuck plan namespace cert-manager

# Include force actions for stubborn cases
unstuck plan namespace cert-manager --max-escalation=4 --allow-force

# Only show informational actions
unstuck plan namespace cert-manager --max-escalation=0
```

### apply

Execute the remediation plan.

```bash
unstuck apply namespace <name> [flags]
```

**Flags:**
- `--dry-run`: Show what would be done without making changes
- `--yes` / `-y`: Skip confirmation prompts for high-risk actions
- `--continue-on-error`: Continue executing even if some actions fail
- `--max-escalation`: Maximum escalation level (0-4, default: 2)
- `--allow-force`: Allow Level 3-4 actions

**Examples:**

```bash
# Dry-run to see exact commands
unstuck apply namespace cert-manager --dry-run

# Execute with default settings
unstuck apply namespace cert-manager

# Execute without prompts (for automation)
unstuck apply namespace cert-manager --yes

# Force-finalize a namespace (dangerous!)
unstuck apply namespace broken-app --max-escalation=4 --allow-force --yes
```

---

## Common Scenarios

### Scenario 1: Namespace Stuck After Uninstalling an Operator

**Problem:** You uninstalled cert-manager (or similar operator) and now the namespace won't delete.

```bash
# Step 1: Diagnose
$ unstuck diagnose namespace cert-manager

Status: Terminating (since 2h ago)
Root Cause: CR instances stuck with unsatisfied finalizers

BLOCKERS (3 found)
- Certificate/my-cert (cert-manager.io/finalizer)
- Certificate/tls-secret (cert-manager.io/finalizer)
- Issuer/letsencrypt-prod (cert-manager.io/finalizer)

# Step 2: Apply remediation
$ unstuck apply namespace cert-manager

✓ Certificate/my-cert finalizers removed
✓ Certificate/tls-secret finalizers removed
✓ Issuer/letsencrypt-prod finalizers removed

SUCCESS: All actions completed.
```

### Scenario 2: CRD Stuck in Terminating

**Problem:** A CRD is stuck because instances still exist.

```bash
# Step 1: Diagnose
$ unstuck diagnose crd certificates.cert-manager.io

Status: Terminating
Finalizer: customresourcecleanup.apiextensions.k8s.io
Remaining Instances: 5 across 3 namespaces

# Step 2: Plan with instance cleanup
$ unstuck plan crd certificates.cert-manager.io

Actions:
1. [L2] Remove finalizers from Certificate/default/my-cert
2. [L2] Remove finalizers from Certificate/production/api-tls
...

# Step 3: Apply
$ unstuck apply crd certificates.cert-manager.io
```

### Scenario 3: Namespace with Deleted CRD (Discovery Failures)

**Problem:** CRD was deleted before its instances, causing discovery failures.

```bash
$ unstuck diagnose namespace broken-app

Status: Terminating
Root Cause: Discovery failures - CRD may have been deleted

CONDITIONS
• NamespaceDeletionDiscoveryFailure: True
  Message: "widgets.example.com/v1: the server could not find the requested resource"

RECOMMENDATION
Force-finalize namespace required. Use --max-escalation=4 --allow-force
```

```bash
# Force-finalize (use with caution!)
$ unstuck apply namespace broken-app --max-escalation=4 --allow-force

🚨 DANGER: Force Namespace Finalization
Type 'FORCE-FINALIZE broken-app' to confirm: FORCE-FINALIZE broken-app

✓ Namespace "broken-app" force-finalized
```

### Scenario 4: Webhook Blocking Operations

**Problem:** A webhook is denying patch operations.

```bash
$ unstuck apply namespace my-app

✗ Certificate/my-app/web-tls FAILED

ERROR: Webhook is blocking patch operations

Webhook: cert-manager-webhook
Error: admission webhook denied the request

To proceed, manually resolve the webhook:
  Option 1: Restore the webhook service
    kubectl rollout restart deployment/cert-manager-webhook -n cert-manager
  
  Option 2: Delete the webhook (use with caution)
    kubectl delete validatingwebhookconfiguration cert-manager-webhook
```

---

## Understanding Output

### Diagnosis Report

```
DIAGNOSIS: Namespace "cert-manager"
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Status:         Terminating (since 2h ago)    <- Current state
Root Cause:     CR instances stuck            <- Why it's stuck

BLOCKERS (3 found)                            <- Resources blocking deletion
┌────┬─────────────────────┬────────────────────────────┬──────────┐
│ #  │ Resource            │ Finalizer                  │ Status   │
├────┼─────────────────────┼────────────────────────────┼──────────┤
│ 1  │ Certificate/my-cert │ cert-manager.io/finalizer  │ Stuck    │
└────┴─────────────────────┴────────────────────────────┴──────────┘
```

### Plan Output

```
REMEDIATION PLAN: Namespace "cert-manager"
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Risk Level:     MEDIUM                         <- Overall risk
Max Escalation: Level 2                        <- Highest action level

ACTIONS (3 steps)
┌────┬───────┬─────────────────────────┬──────────┬────────┐
│ #  │ Level │ Target                  │ Action   │ Risk   │
├────┼───────┼─────────────────────────┼──────────┼────────┤
│ 1  │ L2    │ Certificate/my-cert     │ patch    │ medium │
└────┴───────┴─────────────────────────┴──────────┴────────┘
```

### Apply Result

```
RESULT SUMMARY
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Total Actions:  3
Succeeded:      3
Failed:         0
Skipped:        0
Duration:       1.2s

✅ All actions completed successfully
```

---

## Flags Reference

### Global Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--kubeconfig` | string | `~/.kube/config` | Path to kubeconfig file |
| `--context` | string | current | Kubernetes context to use |
| `--output` / `-o` | string | auto | Output format: `text`, `json` |
| `--verbose` / `-v` | bool | false | Verbose output with audit info |
| `--timeout` | duration | `5m` | Overall operation timeout |

### Command-Specific Flags

| Flag | Commands | Description |
|------|----------|-------------|
| `--max-escalation` | plan, apply | Max escalation level (0-4) |
| `--allow-force` | plan, apply | Allow Level 3-4 actions |
| `--dry-run` | apply | Show what would be done |
| `--yes` / `-y` | apply | Skip confirmation prompts |
| `--continue-on-error` | apply | Continue on action failure |
| `-n` / `--namespace` | diagnose | Namespace for resource targets |

---

## Troubleshooting

### "RBAC prevents operations"

You need additional permissions. Grant them with:

```bash
kubectl create clusterrolebinding unstuck-binding \
  --clusterrole=cluster-admin \
  --user=<your-user>
```

Or create a limited role with only necessary permissions.

### "Webhook is blocking operations"

See [Scenario 4](#scenario-4-webhook-blocking-operations) above. You'll need to either:
1. Restore the webhook service
2. Temporarily delete the webhook configuration

### "Discovery failures"

This usually means a CRD was deleted before its instances. Use:

```bash
unstuck apply namespace <name> --max-escalation=4 --allow-force
```

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | All actions succeeded (or resource is healthy) |
| 1 | All actions failed or fatal error |
| 2 | Partial success (some actions failed) |

---

## Best Practices

1. **Always diagnose first** - Understand what's stuck before remediating
2. **Use dry-run** - Preview changes before applying them
3. **Start with low escalation** - Use Level 2 before resorting to force actions
4. **Check webhooks** - If patches fail, check for blocking webhooks
5. **Document force actions** - If you use Level 3-4, document what was done

---

## Getting Help

```bash
# General help
unstuck --help

# Command-specific help
unstuck diagnose --help
unstuck plan --help
unstuck apply --help
```

For more information, see:
- [Risk Levels Documentation](RISK_LEVELS.md)
- [Project Repository](https://github.com/sozercan/unstuck)
