# Unstuck: Kubernetes Stuck Resource Remediation Tool

## Executive Summary

A CLI tool to diagnose and remediate Kubernetes resources stuck in `Terminating` state due to unsatisfiable finalizers, missing controllers, deleted CRDs, or blocking webhooks.

---

## Table of Contents

1. [Problem Statement](#problem-statement)
2. [Goals and Non-Goals](#goals-and-non-goals)
3. [Design Decisions](#design-decisions)
4. [Architecture Overview](#architecture-overview)
5. [CLI Design](#cli-design)
6. [Detection Logic](#detection-logic)
7. [Escalation Levels](#escalation-levels)
8. [Execution Engine](#execution-engine)
9. [Edge Cases](#edge-cases)
10. [Implementation Phases](#implementation-phases)
11. [CI/CD Configuration](#cicd-configuration)
12. [Testing Strategy](#testing-strategy)
13. [Risk Considerations](#risk-considerations)
14. [Example Usage](#example-usage)

---

## Problem Statement

### The Core Issue

Kubernetes resources can get stuck in `Terminating` when they contain finalizers that cannot be satisfied. This commonly occurs when:

- **Custom Resources (CRs)** have finalizers that should be removed by an operator/controller that is:
  - Uninstalled
  - Crashlooping
  - Blocked by broken webhooks or RBAC

### The Deletion Deadlock

A frequent escalation occurs when a CRD is deleted while CR instances still exist:

```
┌─────────────────────────────────────────────────────────────────┐
│                     DELETION DEADLOCK                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  Namespace (Terminating)                                        │
│       │                                                         │
│       ├── blocked by ──► CR instances (Terminating)             │
│       │                       │                                 │
│       │                       └── blocked by ──► Missing        │
│       │                                          Controller     │
│       │                                                         │
│       └── blocked by ──► CRD (Terminating)                      │
│                              │                                  │
│                              └── blocked by ──► Finalizer:      │
│                                   customresourcecleanup.        │
│                                   apiextensions.k8s.io          │
│                                   (waiting for CR deletion)     │
│                                                                 │
│  + Webhooks may block patch/delete operations                   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Why This Needs a Tool

Manual remediation is:
- **Error-prone**: Wrong order of operations can orphan data or break cluster state
- **Time-consuming**: Requires multiple kubectl commands and JSON parsing
- **Risky**: Force-finalization without understanding blockers can cause issues
- **Repetitive**: Same patterns occur across clusters and teams

---

## Goals and Non-Goals

### Primary Goals

| Goal | Description |
|------|-------------|
| **Diagnose** | Identify why a namespace/CRD/resource is stuck in Terminating |
| **Identify Blockers** | Surface remaining resources, finalizers, discovery failures, and webhook blocks |
| **Plan Remediation** | Produce ordered steps with escalating risk levels |
| **Execute Safely** | Optionally apply fixes with dry-run support and audit logging |

### Non-Goals (Explicit)

| Non-Goal | Rationale |
|----------|-----------|
| General cluster cleanup | Scope creep; different problem space |
| Security bypass | Must respect RBAC; surface permission failures clearly |
| Application state repair | Only unblock API object deletion, not fix app logic |
| Webhook auto-disabling | Too dangerous; users should manually disable webhooks |

---

## Design Decisions

All design decisions have been finalized. This section serves as the authoritative reference.

### Summary Table

| # | Decision Area | Choice | Details |
|---|---------------|--------|---------|
| 1 | Escalation ordering | L3 (CRD) → L4 (Namespace) | CRD finalizer removal before namespace force-finalize |
| 2 | Default `--max-escalation` | Level 2 | Allows targeted finalizer removal without `--allow-force` |
| 3 | Confirmation prompts | Level 3+ only | L2 actions proceed without confirmation; L3/L4 require it |
| 4 | Webhook handling | Detect and report only | No auto-disabling; provide manual remediation guidance |
| 5 | Audit logs | Stdout with `--verbose` | No persistent audit log by default; use verbose output |
| 6 | Multi-cluster support | Deferred to Phase 2 | Single cluster via `--context` for MVP |
| 7 | Output format | Auto-detect TTY | JSON if piped, pretty text if terminal |
| 8 | Resource targeting syntax | kubectl-style | `<resource> <name> -n <namespace>` |
| 9 | Error handling | `--continue-on-error` flag | Default fail-fast; flag to continue |
| 10 | Dry-run output | Structured + kubectl commands | Both action list and copy-paste commands |
| 11 | Exit codes | 0/1/2 | 0=success, 1=failure, 2=partial success |
| 12 | Stdin support | `--from-stdin` flag | Explicit opt-in for piped input |
| 13 | Progress display | Progress bar with counts | `[=====>    ] 50/100 resources` |
| 14 | Config file | Deferred to Phase 2 | Flags only for MVP |
| 15 | CRD namespace scope | All namespaces by default | Scan all instances when diagnosing CRD |
| 16 | Timeout handling | Fail and continue | Timeout treated as error for that action |
| 17 | Parallel execution | Sequential default | `--concurrency=N` flag for parallel |
| 18 | Min Kubernetes version | 1.25+ | No legacy API compatibility |
| 19 | Non-terminating target | Report healthy | Exit 0 with "not stuck" status |
| 20 | Installation | All methods | GitHub releases, Homebrew, Krew |

### Decision Details

#### Escalation Level Ordering (Decision 1)

```
Level 0: Informational (read-only)
Level 1: Clean deletion path (controller exists)
Level 2: Targeted finalizer removal on CR instances
Level 3: CRD cleanup finalizer removal (--allow-force required)
Level 4: Force-finalize namespace (--allow-force required)
```

Rationale: Force-finalizing the namespace is the true nuclear option. CRD finalizer removal is destructive but scoped to one resource type.

#### Output Auto-Detection (Decision 7)

```go
func detectOutputFormat() string {
    if isatty.IsTerminal(os.Stdout.Fd()) {
        return "text"  // Pretty tables with colors
    }
    return "json"  // Machine-readable
}
```

#### Exit Code Semantics (Decision 11)

| Exit Code | Meaning | Example |
|-----------|---------|---------|
| 0 | All actions succeeded | All finalizers removed |
| 1 | All actions failed or fatal error | RBAC denied, resource not found |
| 2 | Partial success | 8/10 finalizers removed, 2 failed |

#### kubectl-Style Targeting (Decision 8)

```bash
# Diagnose a specific resource
unstuck diagnose certificate my-cert -n cert-manager

# Diagnose a CRD
unstuck diagnose crd certificates.cert-manager.io

# Diagnose a namespace
unstuck diagnose namespace cert-manager
```

#### Progress Bar (Decision 13)

```
Scanning namespace cert-manager...
[=================>          ] 67/100 resources | 5 stuck | ETA: 3s
```

---

## Architecture Overview

### High-Level Components

```
┌────────────────────────────────────────────────────────────────┐
│                         CLI Layer                              │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │ diagnose │  │   plan   │  │  apply   │  │  status  │       │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘       │
└───────┼─────────────┼─────────────┼─────────────┼──────────────┘
        │             │             │             │
        ▼             ▼             ▼             ▼
┌────────────────────────────────────────────────────────────────┐
│                      Core Engine                               │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐    │
│  │  Detector   │  │   Planner   │  │      Applier        │    │
│  │             │  │             │  │                     │    │
│  │ - Namespace │  │ - Action    │  │ - Execute actions   │    │
│  │ - CRD       │  │   ordering  │  │ - Verify post-cond  │    │
│  │ - Resource  │  │ - Risk      │  │ - Audit logging     │    │
│  │ - Webhook   │  │   scoring   │  │ - Rollback support  │    │
│  └─────────────┘  └─────────────┘  └─────────────────────┘    │
└────────────────────────────────────────────────────────────────┘
        │             │             │
        ▼             ▼             ▼
┌────────────────────────────────────────────────────────────────┐
│                    Kubernetes Client Layer                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐    │
│  │ Discovery   │  │  Dynamic    │  │    REST Client      │    │
│  │ Client      │  │  Client     │  │ (for /finalize)     │    │
│  └─────────────┘  └─────────────┘  └─────────────────────┘    │
└────────────────────────────────────────────────────────────────┘
```

### Technology Stack

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Language | Go | Native Kubernetes client-go support, single binary |
| K8s Client | client-go | Official client with discovery, dynamic, and REST support |
| CLI Framework | cobra | Standard for K8s ecosystem tools |
| Output | text/tabwriter + JSON | Human and machine-readable |
| Testing | envtest + fake clients | Unit tests without real cluster |
| Min K8s Version | 1.25+ | Modern APIs, no legacy compatibility needed |

### Package Structure

```
unstuck/
├── cmd/
│   └── unstuck/
│       └── main.go              # Entry point
├── pkg/
│   ├── cli/                     # Command definitions
│   │   ├── root.go
│   │   ├── diagnose.go
│   │   ├── plan.go
│   │   └── apply.go
│   ├── detector/                # Detection logic
│   │   ├── detector.go          # Interface
│   │   ├── namespace.go         # Namespace-specific detection
│   │   ├── crd.go               # CRD-specific detection
│   │   ├── resource.go          # Generic resource detection
│   │   └── webhook.go           # Webhook interference detection
│   ├── planner/                 # Remediation planning
│   │   ├── planner.go           # Interface
│   │   ├── actions.go           # Action types
│   │   └── ordering.go          # Dependency-aware ordering
│   ├── applier/                 # Execution engine
│   │   ├── applier.go           # Interface
│   │   ├── executor.go          # Action execution
│   │   ├── verifier.go          # Post-condition verification
│   │   └── audit.go             # Audit logging
│   ├── types/                   # Shared types
│   │   ├── blocker.go
│   │   ├── action.go
│   │   └── report.go
│   └── output/                  # Output formatting
│       ├── text.go
│       └── json.go
├── docs/
│   ├── PLAN.md                  # This document
│   ├── USER_GUIDE.md
│   └── RISK_LEVELS.md
└── test/
    ├── fixtures/                # Test objects with finalizers
    └── e2e/                     # End-to-end tests
```

---

## CLI Design

### Command Structure

```
unstuck <command> <target-type> <target-name> [flags]
```

### Commands

#### `diagnose` - Analyze stuck resources (default, read-only)

```bash
# Diagnose a stuck namespace
unstuck diagnose namespace cert-manager

# Diagnose a stuck CRD
unstuck diagnose crd certificates.cert-manager.io

# Diagnose a specific resource (kubectl-style syntax)
unstuck diagnose certificate my-cert -n cert-manager
```

**Non-terminating resources**: If the target is not in Terminating state, the tool reports "healthy" status and exits 0.

#### `plan` - Generate remediation plan

```bash
# Generate plan with default max escalation (Level 2)
unstuck plan namespace cert-manager

# Limit to informational only
unstuck plan namespace cert-manager --max-escalation=0

# Allow force-level actions in plan
unstuck plan namespace cert-manager --max-escalation=4 --allow-force
```

#### `apply` - Execute remediation

```bash
# Dry-run (show what would be done)
unstuck apply namespace cert-manager --dry-run

# Execute with confirmation prompts
unstuck apply namespace cert-manager

# Execute force actions (requires explicit flag)
unstuck apply namespace cert-manager --allow-force

# Skip confirmation prompts (for automation)
unstuck apply namespace cert-manager --yes
```

#### `status` - Check progress of ongoing remediation

```bash
unstuck status namespace cert-manager
```

### Global Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--kubeconfig` | string | `~/.kube/config` | Path to kubeconfig |
| `--context` | string | current | Kubernetes context to use |
| `--output` / `-o` | string | auto | Output format: `text`, `json`, `yaml` (auto-detects TTY) |
| `--verbose` / `-v` | bool | false | Verbose output (includes audit info) |
| `--timeout` | duration | `5m` | Overall operation timeout |

### Command-Specific Flags

| Flag | Commands | Type | Default | Description |
|------|----------|------|---------|-------------|
| `--max-escalation` | plan, apply | int | 2 | Maximum escalation level (0-4) |
| `--allow-force` | plan, apply | bool | false | Allow Level 3-4 actions |
| `--dry-run` | apply | bool | false | Show actions without executing |
| `--yes` / `-y` | apply | bool | false | Skip confirmation prompts (L3+) |
| `--continue-on-error` | apply | bool | false | Continue on action failure |
| `--concurrency` | apply | int | 1 | Parallel action execution (1=sequential) |
| `--from-stdin` | diagnose, plan, apply | bool | false | Read targets from stdin |
| `--scope` | diagnose, plan | string | "" | Label selector to limit scope |
| `-n` / `--namespace` | diagnose (resource) | string | "" | Namespace for resource targets |

### Output Examples

#### Diagnose Output (Text)

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

ADDITIONAL FINDINGS
• Namespace finalizer: kubernetes
• Controller deployment "cert-manager" not found in namespace "cert-manager"
• No webhook blocking detected

RECOMMENDATION
Run `unstuck plan namespace cert-manager` to see remediation options.
```

#### Dry-Run Output Example

```
$ unstuck apply namespace cert-manager --dry-run

DRY RUN: No changes will be made
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ACTIONS (3 total)
┌────────────┬────────────────────────────────────┬──────────┬──────────┐
│ ID         │ Target                             │ Action   │ Risk     │
├────────────┼────────────────────────────────────┼──────────┼──────────┤
│ action-001 │ Certificate/cert-manager/my-cert   │ patch    │ medium   │
│ action-002 │ Certificate/cert-manager/other     │ patch    │ medium   │
│ action-003 │ Issuer/cert-manager/letsencrypt    │ patch    │ medium   │
└────────────┴────────────────────────────────────┴──────────┴──────────┘

KUBECTL COMMANDS
# action-001: Remove finalizers from Certificate/my-cert
kubectl patch certificate my-cert -n cert-manager \
  -p '{"metadata":{"finalizers":null}}' --type=merge

# action-002: Remove finalizers from Certificate/other
kubectl patch certificate other -n cert-manager \
  -p '{"metadata":{"finalizers":null}}' --type=merge

# action-003: Remove finalizers from Issuer/letsencrypt
kubectl patch issuer letsencrypt -n cert-manager \
  -p '{"metadata":{"finalizers":null}}' --type=merge
```

#### Plan Output (JSON)

```json
{
  "target": {
    "type": "namespace",
    "name": "cert-manager"
  },
  "diagnosis": {
    "status": "Terminating",
    "rootCause": "CR instances stuck with unsatisfied finalizers",
    "deletionTimestamp": "2024-01-09T10:00:00Z"
  },
  "blockers": [
    {
      "kind": "Certificate",
      "apiVersion": "cert-manager.io/v1",
      "namespace": "cert-manager",
      "name": "my-cert",
      "finalizers": ["cert-manager.io/finalizer"],
      "deletionTimestamp": "2024-01-09T10:00:00Z",
      "ownerReferences": []
    }
  ],
  "plan": {
    "riskLevel": "medium",
    "maxEscalation": 2,
    "actions": [
      {
        "id": "action-001",
        "type": "patch",
        "escalationLevel": 2,
        "target": {
          "kind": "Certificate",
          "apiVersion": "cert-manager.io/v1",
          "namespace": "cert-manager",
          "name": "my-cert"
        },
        "operation": "remove-finalizers",
        "command": "kubectl patch certificate my-cert -n cert-manager -p '{\"metadata\":{\"finalizers\":null}}' --type=merge",
        "risk": "medium",
        "requiresForce": false,
        "expectedResult": "Finalizers removed, object deletion proceeds"
      }
    ]
  },
  "commands": [
    "kubectl patch certificate my-cert -n cert-manager -p '{\"metadata\":{\"finalizers\":null}}' --type=merge"
  ]
}
```

---

## Detection Logic

### Overview

The detector must handle three primary target types, each with specific inspection logic.

### A) Namespace Detection

```go
type NamespaceDetector struct {
    client    kubernetes.Interface
    dynamic   dynamic.Interface
    discovery discovery.DiscoveryInterface
}

func (d *NamespaceDetector) Detect(ctx context.Context, name string) (*DiagnosisReport, error)
```

#### Step 1: Fetch and Inspect Namespace

```yaml
# Key fields to inspect
metadata:
  deletionTimestamp: "2024-01-09T10:00:00Z"  # Confirms terminating
  finalizers:
    - kubernetes  # Standard namespace finalizer
spec:
  finalizers:
    - kubernetes
status:
  conditions:
    - type: NamespaceContentRemaining
      status: "True"
      message: "Some resources are remaining: certificates.cert-manager.io has 2 resource instances"
    - type: NamespaceFinalizersRemaining
      status: "True"
      message: "Some content in the namespace has finalizers remaining: cert-manager.io/finalizer in 2 resource instances"
    - type: NamespaceDeletionDiscoveryFailure
      status: "True"
      message: "Discovery failed for some groups..."
```

#### Step 2: Parse Conditions

| Condition | Meaning | Action |
|-----------|---------|--------|
| `NamespaceContentRemaining` | Resources still exist | Enumerate and list |
| `NamespaceFinalizersRemaining` | Resources have finalizers | Identify stuck finalizers |
| `NamespaceDeletionDiscoveryFailure` | CRD/API issues | Check for deleted CRDs |
| `NamespaceDeletionGroupVersionParsingFailure` | Malformed GVs | May need force-finalize |

#### Step 3: Enumerate Remaining Resources

```go
// Pseudo-code for resource enumeration
func (d *NamespaceDetector) enumerateResources(ctx context.Context, namespace string) ([]Resource, error) {
    // 1. Get all namespaced API resources
    resources, _ := d.discovery.ServerPreferredNamespacedResources()
    
    var remaining []Resource
    for _, apiResource := range resources {
        // 2. Skip resources that don't support list
        if !containsVerb(apiResource.Verbs, "list") {
            continue
        }
        
        // 3. List objects in namespace
        gvr := schema.GroupVersionResource{...}
        list, err := d.dynamic.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
        
        if err != nil {
            // Record discovery failure
            continue
        }
        
        // 4. For each object, extract blocker info
        for _, item := range list.Items {
            if hasFinalizersOrDeletionTimestamp(item) {
                remaining = append(remaining, extractResourceInfo(item))
            }
        }
    }
    return remaining, nil
}
```

### B) CRD Detection

#### Step 1: Fetch and Inspect CRD

```yaml
metadata:
  name: certificates.cert-manager.io
  deletionTimestamp: "2024-01-09T10:00:00Z"
  finalizers:
    - customresourcecleanup.apiextensions.k8s.io  # Built-in cleanup finalizer
```

#### Step 2: Count Remaining Instances

```go
func (d *CRDDetector) countInstances(ctx context.Context, crd *apiextensionsv1.CustomResourceDefinition) (int, []Resource, error) {
    // Build GVR from CRD
    gvr := schema.GroupVersionResource{
        Group:    crd.Spec.Group,
        Version:  getStoredVersion(crd),
        Resource: crd.Spec.Names.Plural,
    }
    
    // List across all namespaces (or cluster-scoped)
    var opts metav1.ListOptions
    if crd.Spec.Scope == apiextensionsv1.NamespaceScoped {
        // List in all namespaces
        list, _ := d.dynamic.Resource(gvr).List(ctx, opts)
        return len(list.Items), extractResources(list), nil
    } else {
        list, _ := d.dynamic.Resource(gvr).List(ctx, opts)
        return len(list.Items), extractResources(list), nil
    }
}
```

### C) Webhook Interference Detection

#### When to Check

- When patch/delete operations fail
- When resources have been stuck for a long time
- Proactively during diagnosis

#### Detection Logic

```go
func (d *WebhookDetector) Detect(ctx context.Context, gvr schema.GroupVersionResource) (*WebhookReport, error) {
    report := &WebhookReport{}
    
    // 1. List ValidatingWebhookConfigurations
    vwcs, _ := d.client.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
    
    for _, vwc := range vwcs.Items {
        for _, webhook := range vwc.Webhooks {
            if matchesGVR(webhook.Rules, gvr) {
                // Check if webhook service is healthy
                healthy := d.checkWebhookHealth(ctx, webhook.ClientConfig)
                report.Webhooks = append(report.Webhooks, WebhookInfo{
                    Name:      vwc.Name,
                    Webhook:   webhook.Name,
                    Healthy:   healthy,
                    MatchedOn: explainMatch(webhook.Rules, gvr),
                })
            }
        }
    }
    
    // 2. Same for MutatingWebhookConfigurations
    // ...
    
    return report, nil
}
```

#### Error Pattern Detection

```go
var webhookErrorPatterns = []struct {
    Pattern string
    Type    string
}{
    {`admission webhook .* denied the request`, "webhook_denied"},
    {`failed calling webhook`, "webhook_failed"},
    {`context deadline exceeded`, "webhook_timeout"},
    {`connection refused`, "webhook_unavailable"},
    {`no endpoints available`, "webhook_no_endpoints"},
}
```

---

## Escalation Levels

### Level Overview

| Level | Name | Risk | Requires Force | Typical Use Case |
|-------|------|------|----------------|------------------|
| 0 | Informational | None | No | Diagnosis only, report findings |
| 1 | Clean Deletion | Low | No | Controller exists, retry deletion |
| 2 | Finalizer Removal | Medium | No | Remove finalizers from stuck instances |
| 3 | CRD Finalizer Removal | High | Yes | Remove CRD cleanup finalizer |
| 4 | Force Finalize Namespace | Critical | Yes | Force namespace finalization endpoint |

### Level 0: Informational

**When**: Default for `diagnose` command

**Actions**:
- Report what remains
- Identify finalizers blocking deletion
- Check for controller/operator presence
- Surface webhook interference
- Recommend next steps

**Output**: Diagnosis report only, no modifications

### Level 1: Clean Deletion Path

**When**: Controller/operator can be restored or is running

**Actions**:
```yaml
- id: L1-001
  type: check
  description: Verify controller deployment exists and is healthy
  
- id: L1-002
  type: wait
  description: Wait for controller to process finalizers
  timeout: 60s
  
- id: L1-003
  type: delete
  description: Re-attempt delete of stuck resources
  target: stuck-resource
```

**Prerequisites**:
- Controller deployment exists
- Webhook services are healthy

### Level 2: Targeted Finalizer Removal

**When**: Small number of objects blocking deletion, controller cannot be restored

**Actions**:
```yaml
- id: L2-001
  type: patch
  description: Remove finalizers from Certificate/my-cert
  target:
    kind: Certificate
    name: my-cert
    namespace: cert-manager
  patch:
    type: merge
    body: '{"metadata":{"finalizers":null}}'
```

**Important Considerations**:

1. **Owner Reference Ordering**: Delete children before parents
   ```go
   func orderByOwnerRefs(resources []Resource) []Resource {
       // Build dependency graph
       // Topological sort with children first
   }
   ```

2. **Batching**: For many resources, batch operations
   ```go
   const maxConcurrent = 10  // Limit concurrent patches
   ```

3. **Warning**: Skipping controller cleanup may leave external resources orphaned

### Level 3: CRD Finalizer Removal

**When**: CRD stuck with `customresourcecleanup.apiextensions.k8s.io` and instances can't be cleaned

**Danger**: Removing this finalizer **orphans CR data in etcd**. The CRD will be deleted, but instance data may remain inaccessible.

**Actions**:
```yaml
- id: L3-001
  type: patch
  description: Remove cleanup finalizer from CRD
  target:
    kind: CustomResourceDefinition
    name: certificates.cert-manager.io
  patch:
    type: json
    body: '[{"op":"remove","path":"/metadata/finalizers"}]'
  warning: |
    This will abandon automatic instance cleanup.
    CR data may be orphaned in etcd.
```

**Requirements**:
- `--allow-force` flag
- Interactive confirmation (unless `--yes`)

### Level 4: Force-Finalize Namespace

**When**:
- Namespace stuck due to unlistable resources
- CRDs deleted but namespace still terminating
- Discovery failures preventing normal cleanup

**Actions**:
```yaml
- id: L4-001
  type: finalize
  description: Force-finalize namespace via finalize endpoint
  target:
    kind: Namespace
    name: cert-manager
  method: |
    1. GET /api/v1/namespaces/cert-manager
    2. Remove spec.finalizers
    3. PUT /api/v1/namespaces/cert-manager/finalize
```

**Implementation**:
```go
func (a *Applier) forceFinalize(ctx context.Context, namespace string) error {
    // 1. Get current namespace
    ns, err := a.client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
    if err != nil {
        return err
    }
    
    // 2. Clear finalizers
    ns.Spec.Finalizers = nil
    
    // 3. Use finalize subresource
    _, err = a.client.CoreV1().Namespaces().Finalize(ctx, ns, metav1.UpdateOptions{})
    return err
}
```

**Requirements**:
- `--allow-force` flag
- Interactive confirmation (unless `--yes`)
- Audit log entry

---

## Execution Engine

### Action Types

```go
type ActionType string

const (
    ActionInspect   ActionType = "inspect"   // Read-only check
    ActionList      ActionType = "list"      // Enumerate resources
    ActionPatch     ActionType = "patch"     // Modify object
    ActionDelete    ActionType = "delete"    // Delete object
    ActionFinalize  ActionType = "finalize"  // Force finalize
    ActionWait      ActionType = "wait"      // Wait for condition
)

type Action struct {
    ID              string            `json:"id"`
    Type            ActionType        `json:"type"`
    EscalationLevel int               `json:"escalationLevel"`
    Description     string            `json:"description"`
    Target          ResourceRef       `json:"target"`
    Operation       string            `json:"operation"`
    Command         string            `json:"command"`           // kubectl equivalent
    Risk            RiskLevel         `json:"risk"`
    RequiresForce   bool              `json:"requiresForce"`
    ExpectedResult  string            `json:"expectedResult"`
    DependsOn       []string          `json:"dependsOn,omitempty"`
    Timeout         time.Duration     `json:"timeout,omitempty"`
}

type ApplyResult struct {
    StartTime    time.Time       `json:"startTime"`
    EndTime      time.Time       `json:"endTime"`
    TotalActions int             `json:"totalActions"`
    Succeeded    int             `json:"succeeded"`
    Failed       int             `json:"failed"`
    Skipped      int             `json:"skipped"`
    Actions      []ActionResult  `json:"actions"`
    ExitCode     int             `json:"exitCode"`  // 0=success, 1=failure, 2=partial
}
```

### Planner Logic

```go
type Planner struct {
    maxEscalation int
    allowForce    bool
}

func (p *Planner) Plan(diagnosis *DiagnosisReport) (*Plan, error) {
    var actions []Action
    
    // 1. Always start with informational actions
    actions = append(actions, p.infoActions(diagnosis)...)
    
    if p.maxEscalation >= 1 {
        // 2. Try clean path if controller exists
        if diagnosis.ControllerAvailable {
            actions = append(actions, p.cleanPathActions(diagnosis)...)
        }
    }
    
    if p.maxEscalation >= 2 {
        // 3. Plan finalizer removal for stuck resources
        for _, blocker := range diagnosis.Blockers {
            actions = append(actions, p.finalizerRemovalAction(blocker))
        }
    }
    
    if p.maxEscalation >= 3 && p.allowForce {
        // 4. CRD finalizer removal if applicable
        if diagnosis.CRDBlocked {
            actions = append(actions, p.crdFinalizerAction(diagnosis.CRD))
        }
    }
    
    if p.maxEscalation >= 4 && p.allowForce {
        // 5. Force-finalize as last resort
        if diagnosis.TargetType == "namespace" {
            actions = append(actions, p.forceFinalize(diagnosis.TargetName))
        }
    }
    
    // Order actions by dependencies
    return &Plan{
        Actions:   orderActions(actions),
        RiskLevel: calculateRisk(actions),
    }, nil
}
```

### Dependency-Aware Ordering

```go
func orderActions(actions []Action) []Action {
    // Build dependency graph from ownerReferences
    graph := buildDependencyGraph(actions)
    
    // Topological sort: children before parents
    sorted := topologicalSort(graph)
    
    // Group by escalation level
    return groupByEscalation(sorted)
}
```

### Applier Logic

```go
type Applier struct {
    client          kubernetes.Interface
    dynamic         dynamic.Interface
    dryRun          bool
    continueOnError bool
    concurrency     int
}

func (a *Applier) Apply(ctx context.Context, plan *Plan) (*ApplyResult, error) {
    result := &ApplyResult{
        StartTime:    time.Now(),
        TotalActions: len(plan.Actions),
    }
    
    for _, action := range plan.Actions {
        // 1. Require confirmation for high-risk actions (Level 3+)
        if action.EscalationLevel >= 3 && !a.dryRun {
            if !a.confirm(action) {
                result.Skipped++
                continue
            }
        }
        
        // 2. Record before state
        before, _ := a.snapshot(ctx, action.Target)
        
        // 3. Execute action
        var err error
        if a.dryRun {
            a.logDryRun(action)
        } else {
            err = a.executeWithTimeout(ctx, action)
        }
        
        // 4. Handle errors
        if err != nil {
            result.Failed++
            result.Actions = append(result.Actions, ActionResult{
                Action: action,
                Error:  err,
            })
            if !a.continueOnError {
                break  // Fail fast
            }
            continue
        }
        
        // 5. Verify post-condition
        result.Succeeded++
        after, _ := a.snapshot(ctx, action.Target)
        result.Actions = append(result.Actions, ActionResult{
            Action:     action,
            Before:     before,
            After:      after,
            ExecutedAt: time.Now(),
        })
    }
    
    result.EndTime = time.Now()
    result.ExitCode = a.calculateExitCode(result)
    return result, nil
}

func (a *Applier) calculateExitCode(result *ApplyResult) int {
    if result.Failed == 0 {
        return 0  // All succeeded
    }
    if result.Succeeded == 0 {
        return 1  // All failed
    }
    return 2  // Partial success
}
```

### Post-Condition Verification

| Action Type | Post-Condition |
|-------------|----------------|
| `patch` (finalizer removal) | Object has no finalizers |
| `delete` | Object returns 404 |
| `finalize` | Namespace no longer exists or not terminating |

### Audit Log Format

Audit information is output to stdout when `--verbose` is used:

```
$ unstuck apply namespace cert-manager -v

[2024-01-09T12:34:56Z] ACTION action-001
  Target:  Certificate/cert-manager/my-cert
  Command: kubectl patch certificate my-cert -n cert-manager -p '{"metadata":{"finalizers":null}}' --type=merge
  Before:  finalizers=["cert-manager.io/finalizer"], deletionTimestamp="2024-01-09T10:00:00Z"
  After:   finalizers=null, deletionTimestamp="2024-01-09T10:00:00Z"
  Result:  SUCCESS (245ms)

[2024-01-09T12:34:57Z] ACTION action-002
  ...
```

---

## Edge Cases

### 1. CRD Already Deleted, Namespace Still Stuck

**Symptom**: `NamespaceDeletionDiscoveryFailure` condition

**Detection**:
```go
func detectOrphanedCRD(conditions []corev1.NamespaceCondition) []string {
    for _, c := range conditions {
        if c.Type == "NamespaceDeletionDiscoveryFailure" {
            // Parse message for GVR references
            return extractMissingGVRs(c.Message)
        }
    }
    return nil
}
```

**Remediation**: Skip to Level 4 (force-finalize) since resources can't be listed/cleaned

### 2. Webhook Blocks Patch/Delete

**Symptom**: Patch commands fail with admission webhook errors

**Detection**:
```go
func isWebhookBlocking(err error) (bool, *WebhookInfo) {
    msg := err.Error()
    for _, pattern := range webhookErrorPatterns {
        if match := pattern.Regexp.FindStringSubmatch(msg); match != nil {
            return true, &WebhookInfo{
                Name:  extractWebhookName(match),
                Error: pattern.Type,
            }
        }
    }
    return false, nil
}
```

**Remediation Options**:
1. Report webhook name and recommend manual intervention
2. Check if webhook service exists and is healthy
3. **DO NOT** auto-disable webhooks (too dangerous)

**User Guidance**:
```
WARNING: Webhook "cert-manager-webhook" is blocking patch operations.

To proceed, you must manually resolve the webhook:
  Option 1: Restore the webhook service
    kubectl rollout restart deployment/cert-manager-webhook -n cert-manager

  Option 2: Temporarily disable the webhook (use with caution)
    kubectl delete validatingwebhookconfiguration cert-manager-webhook
    
  After resolving, re-run: unstuck apply namespace cert-manager
```

### 3. RBAC Prevents Operations

**Detection**:
```go
func isRBACError(err error) (bool, *RBACInfo) {
    if apierrors.IsForbidden(err) {
        // Parse for specific verb/resource
        return true, parseRBACError(err)
    }
    return false, nil
}
```

**Output**:
```
ERROR: RBAC prevents patching certificates.cert-manager.io

Required permissions:
  - apiGroups: ["cert-manager.io"]
    resources: ["certificates"]
    verbs: ["get", "patch"]

Grant with:
  kubectl create clusterrolebinding terminator-binding \
    --clusterrole=terminator \
    --user=<your-user>
```

### 4. Large Namespaces (Thousands of Resources)

**Challenges**:
- Listing all resources may timeout
- Too many actions to display/confirm

**Mitigations**:

1. **Pagination**:
   ```go
   opts := metav1.ListOptions{
       Limit:    100,
       Continue: continueToken,
   }
   ```

2. **Progress Reporting**:
   ```
   Scanning namespace big-ns...
   [=================>          ] 67/100 resources | 5 stuck | ETA: 3s
   ```

3. **Scope Filtering**:
   ```bash
   unstuck diagnose namespace big-ns --scope="app=stuck-component"
   ```

4. **Parallel Execution** (opt-in):
   ```bash
   unstuck apply namespace big-ns --concurrency=10
   ```

5. **Rate Limiting** (built-in):
   ```go
   const minDelayBetweenPatches = 100 * time.Millisecond
   ```

### 5. Circular Owner References

**Detection**:
```go
func detectCycles(resources []Resource) [][]ResourceRef {
    graph := buildOwnerRefGraph(resources)
    return findCycles(graph)
}
```

**Handling**: Break cycle by removing finalizers in arbitrary order with warning

### 6. Aggregated API Server Resources

**Challenge**: Custom resources served by aggregated API servers may have different availability

**Detection**:
```go
func isAggregatedAPI(gv schema.GroupVersion) bool {
    apiServices, _ := aggregator.List(...)
    for _, as := range apiServices {
        if as.Spec.Group == gv.Group && as.Spec.Version == gv.Version {
            return as.Spec.Service != nil
        }
    }
    return false
}
```

---

## Implementation Phases

### Phase 1: Core Diagnosis (MVP)

**Duration**: 2-3 weeks

**Deliverables**:
- [ ] CLI skeleton with `diagnose` command
- [ ] Namespace detector (conditions parsing, resource enumeration)
- [ ] CRD detector (finalizer inspection, instance counting)
- [ ] Basic text and JSON output
- [ ] Unit tests with fake clients

**Scope**:
- Read-only operations only
- No remediation execution
- Focus on accuracy of detection

### Phase 2: Planning Engine

**Duration**: 2 weeks

**Deliverables**:
- [ ] `plan` command
- [ ] Action type definitions
- [ ] Escalation level logic
- [ ] Dependency-aware ordering (owner references)
- [ ] Risk calculation
- [ ] Command generation (kubectl equivalents)

**Scope**:
- Generate plans, don't execute
- All escalation levels represented
- Integration tests with kind cluster

### Phase 3: Execution Engine

**Duration**: 2-3 weeks

**Deliverables**:
- [ ] `apply` command
- [ ] Action executor (patch, delete, finalize)
- [ ] Post-condition verification
- [ ] Dry-run mode
- [ ] Audit logging
- [ ] Confirmation prompts

**Scope**:
- Safe execution with verification
- Rollback not implemented (log-based recovery)

### Phase 4: Webhook Detection

**Duration**: 1-2 weeks

**Deliverables**:
- [ ] Webhook interference detection
- [ ] Error pattern matching
- [ ] Webhook health checks
- [ ] Clear guidance for manual webhook resolution

**Scope**:
- Detection and reporting only
- No auto-disabling of webhooks

### Phase 5: Polish and Documentation

**Duration**: 1-2 weeks

**Deliverables**:
- [ ] User guide documentation
- [ ] Risk level documentation
- [ ] "Why this happens" educational content
- [ ] Uninstall-order best practices guide
- [ ] Release automation with goreleaser:
  - [ ] GitHub Releases (binary downloads)
  - [ ] Homebrew tap (`brew install sozercan/tap/terminator`)
  - [ ] Krew plugin manifest (`kubectl krew install terminator`)
- [ ] Installation instructions for all methods
- [ ] CI/CD pipeline (GitHub Actions) — see [CI/CD Configuration](#cicd-configuration)

---

## CI/CD Configuration

### GitHub Actions Workflows

#### Unit Tests (`ci.yml`)

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read

jobs:
  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true

      - name: golangci-lint
        uses: golangci/golangci-lint-action@v4
        with:
          version: latest
          args: --timeout=5m

  unit-tests:
    name: Unit Tests
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true

      - name: Run unit tests
        run: |
          go test -v -race -coverprofile=coverage.out -covermode=atomic ./...

      - name: Upload coverage
        uses: codecov/codecov-action@v4
        with:
          files: ./coverage.out
          fail_ci_if_error: false

  build:
    name: Build
    runs-on: ubuntu-latest
    strategy:
      matrix:
        goos: [linux, darwin, windows]
        goarch: [amd64, arm64]
        exclude:
          - goos: windows
            goarch: arm64
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true

      - name: Build
        env:
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
        run: |
          go build -o terminator-${{ matrix.goos }}-${{ matrix.goarch }} ./cmd/terminator
```

#### Integration Tests with Kind (`integration.yml`)

```yaml
name: Integration Tests

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read

jobs:
  integration-tests:
    name: Integration Tests (k8s ${{ matrix.k8s-version }})
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false
      matrix:
        k8s-version:
          - v1.28.13
          - v1.29.8
          - v1.30.4
          - v1.31.0
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true

      - name: Build terminator
        run: go build -o bin/terminator ./cmd/terminator

      - name: Create kind cluster
        uses: helm/kind-action@v1
        with:
          cluster_name: unstuck-test
          node_image: kindest/node:${{ matrix.k8s-version }}
          wait: 120s

      - name: Wait for cluster ready
        run: |
          kubectl wait --for=condition=Ready nodes --all --timeout=120s
          kubectl cluster-info

      - name: Install test CRDs
        run: |
          kubectl apply -f test/fixtures/crds/

      - name: Run integration tests
        run: |
          go test -v -tags=integration -timeout=15m ./test/e2e/...
        env:
          TERMINATOR_BIN: ${{ github.workspace }}/bin/terminator
          KUBECONFIG: ${{ github.workspace }}/.kube/config

      - name: Test CLI - Diagnose healthy namespace
        run: |
          ./bin/unstuck diagnose namespace default
          # Should exit 0 with "healthy" status

      - name: Test CLI - Diagnose non-existent namespace
        run: |
          ./bin/unstuck diagnose namespace does-not-exist 2>&1 || true
          # Should exit 1

      - name: Create stuck namespace scenario
        run: |
          # Create namespace with test resources
          kubectl create namespace test-stuck
          kubectl apply -f test/fixtures/stuck-resources.yaml -n test-stuck
          
          # Delete namespace (will get stuck)
          kubectl delete namespace test-stuck --wait=false
          
          # Wait for namespace to be terminating
          sleep 5
          kubectl get namespace test-stuck -o jsonpath='{.status.phase}' | grep -q Terminating

      - name: Test CLI - Diagnose stuck namespace
        run: |
          ./bin/unstuck diagnose namespace test-stuck -o json | jq .
          ./bin/unstuck diagnose namespace test-stuck | grep -q "Terminating"

      - name: Test CLI - Plan remediation
        run: |
          ./bin/unstuck plan namespace test-stuck
          ./bin/unstuck plan namespace test-stuck -o json | jq '.plan.actions | length'

      - name: Test CLI - Dry-run apply
        run: |
          ./bin/unstuck apply namespace test-stuck --dry-run

      - name: Test CLI - Apply remediation
        run: |
          ./bin/unstuck apply namespace test-stuck --yes
          
          # Verify namespace is gone
          sleep 5
          ! kubectl get namespace test-stuck 2>/dev/null

      - name: Upload logs on failure
        if: failure()
        uses: actions/upload-artifact@v4
        with:
          name: integration-logs-${{ matrix.k8s-version }}
          path: |
            /tmp/unstuck-*.log
```

#### Release Workflow (`release.yml`)

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write
  packages: write

jobs:
  release:
    name: Release
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v5
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}

  krew-update:
    name: Update Krew Plugin
    runs-on: ubuntu-latest
    needs: release
    steps:
      - uses: actions/checkout@v4

      - name: Update Krew manifest
        uses: rajatjindal/krew-release-bot@v0.0.46
        with:
          krew_template_file: .krew.yaml
```

### GoReleaser Configuration (`.goreleaser.yaml`)

```yaml
version: 2

project_name: terminator

before:
  hooks:
    - go mod tidy
    - go generate ./...

builds:
  - id: terminator
    main: ./cmd/terminator
    binary: terminator
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64
    ldflags:
      - -s -w
      - -X main.version={{.Version}}
      - -X main.commit={{.Commit}}
      - -X main.date={{.Date}}

archives:
  - id: terminator
    format: tar.gz
    name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"
    format_overrides:
      - goos: windows
        format: zip

checksum:
  name_template: 'checksums.txt'

snapshot:
  version_template: "{{ incpatch .Version }}-next"

changelog:
  sort: asc
  filters:
    exclude:
      - '^docs:'
      - '^test:'
      - '^ci:'
      - Merge pull request
      - Merge branch

brews:
  - name: terminator
    repository:
      owner: sozercan
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_GITHUB_TOKEN }}"
    folder: Formula
    homepage: "https://github.com/sozercan/unstuck"
    description: "Diagnose and remediate Kubernetes resources stuck in Terminating state"
    license: "Apache-2.0"
    install: |
      bin.install "terminator"
    test: |
      system "#{bin}/terminator", "--version"
```

### Krew Plugin Manifest (`.krew.yaml`)

```yaml
apiVersion: krew.googlecontainertools.github.com/v1alpha2
kind: Plugin
metadata:
  name: terminator
spec:
  version: {{ .TagName }}
  homepage: https://github.com/sozercan/unstuck
  shortDescription: Diagnose and fix stuck Terminating resources
  description: |
    Terminator diagnoses and remediates Kubernetes resources stuck in 
    Terminating state due to unsatisfiable finalizers, missing controllers, 
    deleted CRDs, or blocking webhooks.
    
    Features:
    - Diagnose stuck namespaces, CRDs, and resources
    - Generate remediation plans with escalating risk levels
    - Execute fixes with dry-run support and audit logging
    
    Usage:
      kubectl unstuck diagnose namespace <name>
      kubectl unstuck plan namespace <name>
      kubectl unstuck apply namespace <name> --dry-run
  caveats: |
    This plugin can remove finalizers from resources, which may cause
    external resources to be orphaned. Use with caution.
  platforms:
    - selector:
        matchLabels:
          os: linux
          arch: amd64
      uri: https://github.com/sozercan/unstuck/releases/download/{{ .TagName }}/terminator_linux_amd64.tar.gz
      sha256: {{ .Sha256.linux_amd64 }}
      bin: terminator
    - selector:
        matchLabels:
          os: linux
          arch: arm64
      uri: https://github.com/sozercan/unstuck/releases/download/{{ .TagName }}/terminator_linux_arm64.tar.gz
      sha256: {{ .Sha256.linux_arm64 }}
      bin: terminator
    - selector:
        matchLabels:
          os: darwin
          arch: amd64
      uri: https://github.com/sozercan/unstuck/releases/download/{{ .TagName }}/terminator_darwin_amd64.tar.gz
      sha256: {{ .Sha256.darwin_amd64 }}
      bin: terminator
    - selector:
        matchLabels:
          os: darwin
          arch: arm64
      uri: https://github.com/sozercan/unstuck/releases/download/{{ .TagName }}/terminator_darwin_arm64.tar.gz
      sha256: {{ .Sha256.darwin_arm64 }}
      bin: terminator
    - selector:
        matchLabels:
          os: windows
          arch: amd64
      uri: https://github.com/sozercan/unstuck/releases/download/{{ .TagName }}/terminator_windows_amd64.zip
      sha256: {{ .Sha256.windows_amd64 }}
      bin: terminator.exe
```

### Test Fixtures for Integration Tests

#### `test/fixtures/crds/testresource-crd.yaml`

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: testresources.example.com
spec:
  group: example.com
  names:
    kind: TestResource
    listKind: TestResourceList
    plural: testresources
    singular: testresource
    shortNames:
      - tr
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                data:
                  type: string
```

#### `test/fixtures/stuck-resources.yaml`

```yaml
apiVersion: example.com/v1
kind: TestResource
metadata:
  name: stuck-resource-1
  finalizers:
    - example.com/test-finalizer
spec:
  data: "This resource will get stuck"
---
apiVersion: example.com/v1
kind: TestResource
metadata:
  name: stuck-resource-2
  finalizers:
    - example.com/test-finalizer
spec:
  data: "This resource will also get stuck"
```

### Makefile Targets

```makefile
.PHONY: test test-unit test-integration lint build

# Run all tests
test: test-unit test-integration

# Run unit tests
test-unit:
	go test -v -race -coverprofile=coverage.out ./pkg/...

# Run integration tests (requires kind cluster)
test-integration:
	@if ! kubectl cluster-info > /dev/null 2>&1; then \
		echo "Creating kind cluster..."; \
		kind create cluster --name unstuck-test; \
	fi
	go test -v -tags=integration -timeout=15m ./test/e2e/...

# Lint
lint:
	golangci-lint run --timeout=5m

# Build
build:
	go build -o bin/terminator ./cmd/terminator

# Create test cluster
test-cluster:
	kind create cluster --name unstuck-test --image kindest/node:v1.30.4

# Delete test cluster
test-cluster-delete:
	kind delete cluster --name unstuck-test
```

---

## Testing Strategy

### Unit Tests

```go
// Example: namespace detector test
func TestNamespaceDetector_TerminatingWithFinalizers(t *testing.T) {
    ns := &corev1.Namespace{
        ObjectMeta: metav1.ObjectMeta{
            Name:              "test-ns",
            DeletionTimestamp: &metav1.Time{Time: time.Now()},
            Finalizers:        []string{"kubernetes"},
        },
        Status: corev1.NamespaceStatus{
            Conditions: []corev1.NamespaceCondition{
                {
                    Type:    "NamespaceContentRemaining",
                    Status:  corev1.ConditionTrue,
                    Message: "certificates.cert-manager.io has 2 resource instances",
                },
            },
        },
    }
    
    client := fake.NewSimpleClientset(ns)
    detector := NewNamespaceDetector(client, nil, nil)
    
    report, err := detector.Detect(context.Background(), "test-ns")
    require.NoError(t, err)
    assert.Equal(t, "Terminating", report.Status)
    assert.Len(t, report.RemainingResources, 1)
}
```

### Integration Tests (kind cluster)

```go
// Example: e2e test for namespace diagnosis
func TestE2E_DiagnoseStuckNamespace(t *testing.T) {
    // Setup: Create namespace with CR and delete controller
    setupStuckNamespace(t)
    
    // Execute diagnosis
    output := runTerminator(t, "diagnose", "namespace", "stuck-ns", "-o", "json")
    
    // Verify
    var report DiagnosisReport
    json.Unmarshal(output, &report)
    
    assert.Equal(t, "Terminating", report.Status)
    assert.Contains(t, report.RootCause, "finalizers")
}
```

### Test Fixtures

Create reusable YAML fixtures:

```yaml
# test/fixtures/stuck-namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: test-stuck-ns
  deletionTimestamp: "2024-01-01T00:00:00Z"
  finalizers:
    - kubernetes
status:
  conditions:
    - type: NamespaceContentRemaining
      status: "True"
      message: "testresources.example.com has 1 resource instances"
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: testresources.example.com
  deletionTimestamp: "2024-01-01T00:00:00Z"
  finalizers:
    - customresourcecleanup.apiextensions.k8s.io
spec:
  group: example.com
  names:
    kind: TestResource
    plural: testresources
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
```

---

## Risk Considerations

### Risk Matrix

| Action | Risk Level | Reversible | Data Loss Potential | Requires Force |
|--------|------------|------------|---------------------|----------------|
| Diagnose | None | N/A | None | No |
| List resources | None | N/A | None | No |
| Retry delete | Low | Yes | None (intended deletion) | No |
| Remove CR finalizers | Medium | No* | Possible orphaned external resources | No |
| Remove CRD finalizer | High | No | Orphaned etcd data | Yes |
| Force-finalize namespace | Critical | No | All remaining resources abandoned | Yes |

*Finalizers cannot be restored once removed and object is deleted

---

## Example Usage

### Scenario 1: Namespace Stuck After Uninstalling cert-manager

```bash
# Step 1: Diagnose the problem
$ unstuck diagnose namespace cert-manager

DIAGNOSIS: Namespace "cert-manager"
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Status:         Terminating (since 2h ago)
Root Cause:     CR instances stuck with unsatisfied finalizers

BLOCKERS (3 found)
┌────┬─────────────────────────────────────┬────────────────────────────────┬──────────┐
│ #  │ Resource                            │ Finalizer                      │ Status   │
├────┼─────────────────────────────────────┼────────────────────────────────┼──────────┤
│ 1  │ Certificate/my-cert                 │ cert-manager.io/finalizer      │ Stuck    │
│ 2  │ Certificate/tls-secret              │ cert-manager.io/finalizer      │ Stuck    │
│ 3  │ Issuer/letsencrypt-prod             │ cert-manager.io/finalizer      │ Stuck    │
└────┴─────────────────────────────────────┴────────────────────────────────┴──────────┘

CONTROLLER STATUS
• Deployment "cert-manager" not found
• Deployment "cert-manager-webhook" not found

RECOMMENDATION
Controller is not running. Use `unstuck plan` to generate remediation steps.

# Step 2: Generate a remediation plan
$ unstuck plan namespace cert-manager

REMEDIATION PLAN: Namespace "cert-manager"
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Risk Level:     MEDIUM
Max Escalation: Level 2 (Finalizer Removal)

ACTIONS (3 steps)
┌────┬───────┬─────────────────────────────────────┬──────────┬────────────────────────────────┐
│ #  │ Level │ Target                              │ Action   │ Description                    │
├────┼───────┼─────────────────────────────────────┼──────────┼────────────────────────────────┤
│ 1  │ L2    │ Certificate/cert-manager/my-cert    │ patch    │ Remove finalizers              │
│ 2  │ L2    │ Certificate/cert-manager/tls-secret │ patch    │ Remove finalizers              │
│ 3  │ L2    │ Issuer/cert-manager/letsencrypt     │ patch    │ Remove finalizers              │
└────┴───────┴─────────────────────────────────────┴──────────┴────────────────────────────────┘

To execute: unstuck apply namespace cert-manager
To dry-run:  unstuck apply namespace cert-manager --dry-run

# Step 3: Dry-run to see exact commands
$ unstuck apply namespace cert-manager --dry-run

DRY RUN: No changes will be made
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

KUBECTL COMMANDS
# Step 1: Remove finalizers from Certificate/my-cert
kubectl patch certificate my-cert -n cert-manager \
  -p '{"metadata":{"finalizers":null}}' --type=merge

# Step 2: Remove finalizers from Certificate/tls-secret
kubectl patch certificate tls-secret -n cert-manager \
  -p '{"metadata":{"finalizers":null}}' --type=merge

# Step 3: Remove finalizers from Issuer/letsencrypt-prod
kubectl patch issuer letsencrypt-prod -n cert-manager \
  -p '{"metadata":{"finalizers":null}}' --type=merge

# Step 4: Execute the remediation
$ unstuck apply namespace cert-manager

Applying remediation plan...
[====================] 3/3 actions | 0 failed

✓ Certificate/my-cert      finalizers removed
✓ Certificate/tls-secret   finalizers removed  
✓ Issuer/letsencrypt-prod  finalizers removed

SUCCESS: All actions completed. Namespace should be deleted shortly.
```

### Scenario 2: CRD Stuck in Terminating

```bash
$ unstuck diagnose crd certificates.cert-manager.io

DIAGNOSIS: CRD "certificates.cert-manager.io"
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Status:         Terminating (since 30m ago)
Root Cause:     CRD cleanup finalizer waiting for instance deletion

FINALIZERS
• customresourcecleanup.apiextensions.k8s.io (built-in)

REMAINING INSTANCES (5 across 3 namespaces)
┌────┬───────────────────────────┬─────────────────┬────────────────────────────────┐
│ #  │ Resource                  │ Namespace       │ Finalizer                      │
├────┼───────────────────────────┼─────────────────┼────────────────────────────────┤
│ 1  │ Certificate/my-cert       │ default         │ cert-manager.io/finalizer      │
│ 2  │ Certificate/api-tls       │ production      │ cert-manager.io/finalizer      │
│ 3  │ Certificate/web-tls       │ production      │ cert-manager.io/finalizer      │
│ 4  │ Certificate/internal      │ staging         │ cert-manager.io/finalizer      │
│ 5  │ Certificate/test-cert     │ staging         │ cert-manager.io/finalizer      │
└────┴───────────────────────────┴─────────────────┴────────────────────────────────┘

RECOMMENDATION
Remove finalizers from all instances first, then CRD will auto-delete.
Use `unstuck plan crd certificates.cert-manager.io` to generate steps.

# Plan with force option to include CRD finalizer removal
$ unstuck plan crd certificates.cert-manager.io --max-escalation=3 --allow-force

REMEDIATION PLAN: CRD "certificates.cert-manager.io"
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Risk Level:     HIGH (includes Level 3 actions)
Max Escalation: Level 3 (CRD Finalizer Removal)

⚠️  WARNING: Level 3 actions require --allow-force and confirmation

ACTIONS (6 steps)
┌────┬───────┬────────────────────────────────────────────┬──────────┬─────────────────────┐
│ #  │ Level │ Target                                     │ Action   │ Risk                │
├────┼───────┼────────────────────────────────────────────┼──────────┼─────────────────────┤
│ 1  │ L2    │ Certificate/default/my-cert                │ patch    │ medium              │
│ 2  │ L2    │ Certificate/production/api-tls             │ patch    │ medium              │
│ 3  │ L2    │ Certificate/production/web-tls             │ patch    │ medium              │
│ 4  │ L2    │ Certificate/staging/internal               │ patch    │ medium              │
│ 5  │ L2    │ Certificate/staging/test-cert              │ patch    │ medium              │
│ 6  │ L3    │ CRD/certificates.cert-manager.io           │ patch    │ HIGH (force req'd)  │
└────┴───────┴────────────────────────────────────────────┴──────────┴─────────────────────┘

Note: Step 6 removes CRD cleanup finalizer. Only needed if instances don't clear.
```

### Scenario 3: Namespace with Discovery Failures (Deleted CRD)

```bash
$ unstuck diagnose namespace broken-app

DIAGNOSIS: Namespace "broken-app"
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Status:         Terminating (since 6h ago)
Root Cause:     Discovery failures - CRD may have been deleted

CONDITIONS
• NamespaceDeletionDiscoveryFailure: True
  Message: "Discovery failed for some groups: unable to retrieve the complete 
           list of server APIs: widgets.example.com/v1: the server could not 
           find the requested resource"

DISCOVERY FAILURES
• widgets.example.com/v1 - API group not found (CRD likely deleted)

⚠️  Cannot enumerate remaining resources due to discovery failures.

RECOMMENDATION
Force-finalize namespace required. Use --max-escalation=4 --allow-force

$ unstuck apply namespace broken-app --max-escalation=4 --allow-force

🚨 DANGER: Force Namespace Finalization

You are about to force-finalize namespace "broken-app".

This is a DESTRUCTIVE operation that:
  • Bypasses all normal cleanup procedures
  • May orphan resources that could not be listed/deleted
  • Cannot be undone

Type 'FORCE-FINALIZE broken-app' to confirm: FORCE-FINALIZE broken-app

Applying force-finalization...

✓ Namespace "broken-app" force-finalized

SUCCESS: Namespace deleted.
```

### Scenario 4: Webhook Blocking Operations

```bash
$ unstuck apply namespace my-app

Applying remediation plan...
[=====>              ] 1/5 actions

✗ Certificate/my-app/web-tls  FAILED

ERROR: Webhook is blocking patch operations

Webhook:  cert-manager-webhook.cert-manager.svc
Error:    admission webhook "webhook.cert-manager.io" denied the request: 
          certificate.spec.secretName is immutable

To proceed, you must manually resolve the webhook:

  Option 1: Restore the webhook service
    kubectl rollout restart deployment/cert-manager-webhook -n cert-manager

  Option 2: Delete the webhook configuration (use with caution)
    kubectl delete validatingwebhookconfiguration cert-manager-webhook

  After resolving, re-run: unstuck apply namespace my-app --continue-on-error
```

### Scenario 5: Healthy Namespace (Not Stuck)

```bash
$ unstuck diagnose namespace kube-system

DIAGNOSIS: Namespace "kube-system"
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Status:         Active ✓
Root Cause:     N/A - Namespace is healthy

The namespace "kube-system" is not in Terminating state.
No remediation needed.
```

### Scenario 6: Piping Multiple Namespaces

```bash
# Find all terminating namespaces and diagnose them
$ kubectl get ns --field-selector=status.phase=Terminating -o name | \
    unstuck diagnose --from-stdin

DIAGNOSIS: Namespace "app-1"
Status: Terminating | Blockers: 2 | Root Cause: CR finalizers

DIAGNOSIS: Namespace "app-2"  
Status: Terminating | Blockers: 1 | Root Cause: CRD deleted

DIAGNOSIS: Namespace "old-system"
Status: Terminating | Blockers: 15 | Root Cause: CR finalizers

Summary: 3 namespaces diagnosed, 18 total blockers found
```

### Scenario 7: JSON Output for Automation

```bash
$ unstuck diagnose namespace cert-manager -o json | jq '.blockers | length'
3

$ unstuck plan namespace cert-manager -o json | jq '.plan.actions[].command'
"kubectl patch certificate my-cert -n cert-manager -p '{\"metadata\":{\"finalizers\":null}}' --type=merge"
"kubectl patch certificate tls-secret -n cert-manager -p '{\"metadata\":{\"finalizers\":null}}' --type=merge"
"kubectl patch issuer letsencrypt-prod -n cert-manager -p '{\"metadata\":{\"finalizers\":null}}' --type=merge"
```

### Scenario 8: Verbose Mode with Audit Output

```bash
$ unstuck apply namespace cert-manager -v

[2026-01-09T10:30:00Z] Starting remediation for namespace "cert-manager"
[2026-01-09T10:30:00Z] Plan: 3 actions, max escalation: Level 2

[2026-01-09T10:30:01Z] ACTION 1/3
  Target:  Certificate/cert-manager/my-cert
  Command: kubectl patch certificate my-cert -n cert-manager -p '{"metadata":{"finalizers":null}}' --type=merge
  Before:  finalizers=["cert-manager.io/finalizer"]
  After:   finalizers=null
  Result:  SUCCESS (127ms)

[2026-01-09T10:30:01Z] ACTION 2/3
  Target:  Certificate/cert-manager/tls-secret
  Command: kubectl patch certificate tls-secret -n cert-manager -p '{"metadata":{"finalizers":null}}' --type=merge
  Before:  finalizers=["cert-manager.io/finalizer"]
  After:   finalizers=null
  Result:  SUCCESS (98ms)

[2026-01-09T10:30:01Z] ACTION 3/3
  Target:  Issuer/cert-manager/letsencrypt-prod
  Command: kubectl patch issuer letsencrypt-prod -n cert-manager -p '{"metadata":{"finalizers":null}}' --type=merge
  Before:  finalizers=["cert-manager.io/finalizer"]
  After:   finalizers=null
  Result:  SUCCESS (112ms)

[2026-01-09T10:30:02Z] Remediation complete
  Total:     3 actions
  Succeeded: 3
  Failed:    0
  Duration:  1.2s
  Exit code: 0
```

---

## Appendix

### Reference: Namespace Finalizer Flow

```
kubectl delete namespace my-ns
         │
         ▼
┌────────────────────────┐
│ deletionTimestamp set  │
│ finalizer: kubernetes  │
└───────────┬────────────┘
            │
            ▼
┌────────────────────────────────────────────┐
│ Namespace controller enumerates resources │
└───────────┬────────────────────────────────┘
            │
    ┌───────┴───────┐
    │               │
    ▼               ▼
┌──────────┐   ┌──────────────────────────────┐
│ Empty?   │   │ Resources found              │
│ Yes      │   │ Wait for deletion...         │
└────┬─────┘   └──────────┬───────────────────┘
     │                    │
     │         ┌──────────┴──────────┐
     │         │                     │
     │         ▼                     ▼
     │  ┌────────────────┐   ┌───────────────────────┐
     │  │ Resources have │   │ Resources stuck       │
     │  │ no finalizers  │   │ (finalizers present)  │
     │  │ → get deleted  │   │ → STUCK               │
     │  └───────┬────────┘   └───────────────────────┘
     │          │
     ▼          ▼
┌────────────────────────┐
│ kubernetes finalizer   │
│ removed automatically  │
└───────────┬────────────┘
            │
            ▼
┌────────────────────────┐
│ Namespace deleted      │
└────────────────────────┘
```

### Reference: kubectl Commands for Manual Remediation

```bash
# Diagnose namespace
kubectl get namespace <ns> -o yaml
kubectl get namespace <ns> -o jsonpath='{.status.conditions}'

# List all resources in namespace
kubectl api-resources --namespaced -o name | \
  xargs -I {} kubectl get {} -n <ns> --ignore-not-found

# Remove finalizers from resource
kubectl patch <kind> <name> -n <ns> \
  -p '{"metadata":{"finalizers":null}}' --type=merge

# Force-finalize namespace
kubectl get namespace <ns> -o json | \
  jq '.spec.finalizers = []' | \
  kubectl replace --raw "/api/v1/namespaces/<ns>/finalize" -f -

# Check CRD finalizers
kubectl get crd <name> -o jsonpath='{.metadata.finalizers}'

# Remove CRD finalizers
kubectl patch crd <name> \
  -p '{"metadata":{"finalizers":null}}' --type=merge
```
