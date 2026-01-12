# Unstuck Risk Levels and Escalation Guide

This document explains the escalation levels used by Unstuck and the risks associated with each.

## Overview

Unstuck uses a 5-level escalation system (L0-L4) to categorize actions by risk and impact. Higher levels require explicit opt-in via `--allow-force` and confirmation prompts.

## Escalation Levels

### Level 0: Informational (No Risk)

**What it does:** Read-only inspection and reporting.

**Actions:**
- Report what resources remain
- Identify finalizers blocking deletion
- Check controller/operator presence
- Detect webhook interference
- Surface RBAC issues

**Risk:** None - no changes are made.

**When to use:** Always start here with `unstuck diagnose`.

```bash
unstuck diagnose namespace my-namespace
```

---

### Level 1: Clean Deletion Path (Low Risk)

**What it does:** Attempts normal deletion when controllers can process finalizers.

**Actions:**
- Verify controller deployment exists and is healthy
- Wait for controller to process finalizers
- Re-attempt delete of stuck resources

**Risk:** Low - uses normal Kubernetes deletion flow.

**Prerequisites:**
- Controller deployment must exist
- Webhook services must be healthy

**When to use:** When the controller is running but just needs a retry.

---

### Level 2: Targeted Finalizer Removal (Medium Risk)

**What it does:** Removes finalizers from stuck custom resource instances.

**Actions:**
```bash
kubectl patch <kind> <name> -n <namespace> \
  -p '{"metadata":{"finalizers":null}}' --type=merge
```

**Risk:** Medium
- Finalizers exist to ensure cleanup of external resources
- Removing them may orphan external resources (cloud resources, databases, etc.)
- Cannot be undone once the object is deleted

**When to use:** 
- Controller is uninstalled or cannot be restored
- Small number of stuck resources
- You understand the external cleanup implications

**Included by default:** Yes (default `--max-escalation=2`)

```bash
unstuck apply namespace my-namespace
```

---

### Level 3: CRD Finalizer Removal (High Risk)

**What it does:** Removes the `customresourcecleanup.apiextensions.k8s.io` finalizer from a CRD.

**Actions:**
```bash
kubectl patch crd <name> \
  -p '{"metadata":{"finalizers":null}}' --type=merge
```

**Risk:** High
- Orphans CR instance data in etcd
- CRD will be deleted, but instance data may remain inaccessible
- May cause issues if CRD is reinstalled later

**When to use:**
- CR instances cannot be cleaned up normally
- You've already tried Level 2 on all instances
- You accept that instance data may be orphaned

**Requires:** `--allow-force` flag and confirmation prompt

```bash
unstuck apply crd my-crd.example.com --max-escalation=3 --allow-force
```

---

### Level 4: Force-Finalize Namespace (Critical Risk)

**What it does:** Calls the namespace finalize endpoint directly, bypassing all normal cleanup.

**Actions:**
```bash
# Equivalent to:
kubectl get namespace <ns> -o json | \
  jq '.spec.finalizers = []' | \
  kubectl replace --raw "/api/v1/namespaces/<ns>/finalize" -f -
```

**Risk:** Critical
- Bypasses ALL normal cleanup procedures
- May orphan resources that couldn't be listed/deleted
- May leave external resources (cloud, databases) without cleanup
- Cannot be undone

**When to use:**
- Namespace has discovery failures (CRD already deleted)
- All other escalation levels have been tried
- You accept complete loss of cleanup for remaining resources

**Requires:** `--allow-force` flag and explicit confirmation

```bash
unstuck apply namespace broken-ns --max-escalation=4 --allow-force

# Will prompt:
# 🚨 DANGER: Force Namespace Finalization
# Type 'FORCE-FINALIZE broken-ns' to confirm:
```

---

## Risk Matrix

| Level | Name | Risk | Reversible | Data Loss | Force Flag |
|-------|------|------|------------|-----------|------------|
| 0 | Informational | None | N/A | None | No |
| 1 | Clean Deletion | Low | Yes | None | No |
| 2 | Finalizer Removal | Medium | No* | Possible orphaned external resources | No |
| 3 | CRD Finalizer Removal | High | No | Orphaned etcd data | Yes |
| 4 | Force-Finalize | Critical | No | All remaining resources abandoned | Yes |

*Once a finalizer is removed and the object is deleted, it cannot be restored.

---

## Understanding Finalizers

### What are Finalizers?

Finalizers are strings in an object's `metadata.finalizers` array that prevent deletion until removed. They're used by controllers to:

1. Clean up external resources (cloud load balancers, DNS records)
2. Update dependent resources
3. Maintain referential integrity

### The Deletion Flow

```
kubectl delete object
       │
       ▼
┌──────────────────────┐
│ deletionTimestamp    │
│ is set               │
└──────────┬───────────┘
           │
           ▼
┌──────────────────────┐
│ Controller sees      │
│ deletion in progress │
└──────────┬───────────┘
           │
           ▼
┌──────────────────────┐
│ Controller performs  │
│ cleanup (external    │
│ resources, etc.)     │
└──────────┬───────────┘
           │
           ▼
┌──────────────────────┐
│ Controller removes   │
│ its finalizer        │
└──────────┬───────────┘
           │
           ▼
┌──────────────────────┐
│ Object deleted when  │
│ all finalizers gone  │
└──────────────────────┘
```

### When Finalizers Get Stuck

Finalizers get stuck when:

1. **Controller uninstalled** - Nothing is watching to remove the finalizer
2. **Controller crashlooping** - Controller can't process the finalizer
3. **RBAC issues** - Controller lacks permissions
4. **Webhook blocking** - Admission webhook denies the patch
5. **External dependency unavailable** - Controller can't complete cleanup

---

## Decision Tree

```
Is the resource stuck in Terminating?
│
├── No → Nothing to do (exit 0)
│
└── Yes → Is the controller running?
          │
          ├── Yes → Try Level 1 (wait/retry)
          │         │
          │         └── Still stuck? → Check webhooks
          │                           │
          │                           ├── Webhook blocking? → Fix webhook manually
          │                           │
          │                           └── No webhook issue → Try Level 2
          │
          └── No → Can controller be restored?
                   │
                   ├── Yes → Restore controller, then Level 1
                   │
                   └── No → Use Level 2
                            │
                            └── CRD also stuck?
                                │
                                ├── No → Level 2 should resolve
                                │
                                └── Yes → Level 3 for CRD
                                          │
                                          └── Discovery failures?
                                              │
                                              ├── No → Level 2+3 should resolve
                                              │
                                              └── Yes → Level 4 (force-finalize)
```

---

## Safe Practices

### Before Using Level 2+

1. **Document what's stuck** - Run `diagnose` and save the output
2. **Understand the finalizers** - Know what cleanup they're supposed to do
3. **Check for external resources** - Cloud consoles, databases, DNS
4. **Use dry-run first** - `--dry-run` shows exactly what will happen

### Before Using Level 3-4

1. **Exhaust lower levels first** - Try L0-L2 before escalating
2. **Verify discovery failures** - For L4, confirm you can't list resources
3. **Accept data loss** - Understand that resources may be orphaned
4. **Document the action** - Keep a record of force operations

---

## Examples by Scenario

### Uninstalled Operator (Level 2)

```bash
# Operator removed, CRs stuck
unstuck diagnose namespace cert-manager
unstuck apply namespace cert-manager  # Uses Level 2 by default
```

### Stuck CRD with Instances (Level 2 + 3)

```bash
# Clean up instances first
unstuck apply crd certificates.cert-manager.io

# If CRD still stuck, force it
unstuck apply crd certificates.cert-manager.io --max-escalation=3 --allow-force
```

### Namespace with Deleted CRD (Level 4)

```bash
# Discovery failures prevent normal cleanup
unstuck diagnose namespace broken-app
# Shows: NamespaceDeletionDiscoveryFailure

# Force-finalize is the only option
unstuck apply namespace broken-app --max-escalation=4 --allow-force
```

---

## See Also

- [User Guide](USER_GUIDE.md) - General usage instructions
- [PLAN.md](PLAN.md) - Technical design document
