package applier

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sozercan/unstuck/pkg/kube"
	"github.com/sozercan/unstuck/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// Executor handles the actual execution of actions
type Executor struct {
	client *kube.Client
}

// NewExecutor creates a new executor
func NewExecutor(client *kube.Client) *Executor {
	return &Executor{client: client}
}

// Execute performs the given action
func (e *Executor) Execute(ctx context.Context, action types.Action) error {
	switch action.Type {
	case types.ActionInspect:
		// Read-only, nothing to execute
		return nil
	case types.ActionList:
		// Read-only, nothing to execute
		return nil
	case types.ActionPatch:
		return e.executePatch(ctx, action)
	case types.ActionDelete:
		return e.executeDelete(ctx, action)
	case types.ActionFinalize:
		return e.executeFinalize(ctx, action)
	case types.ActionWait:
		return e.executeWait(ctx, action)
	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

// executePatch patches a resource to remove finalizers
func (e *Executor) executePatch(ctx context.Context, action types.Action) error {
	target := action.Target

	// Determine if this is a core resource or custom resource
	switch target.Kind {
	case "Namespace":
		return e.patchNamespace(ctx, target.Name)
	case "CustomResourceDefinition":
		return e.patchCRD(ctx, target.Name)
	default:
		return e.patchDynamicResource(ctx, target)
	}
}

// patchNamespace patches a namespace to remove finalizers
func (e *Executor) patchNamespace(ctx context.Context, name string) error {
	patchData := []byte(`{"metadata":{"finalizers":null}}`)
	_, err := e.client.Clientset.CoreV1().Namespaces().Patch(
		ctx,
		name,
		k8stypes.MergePatchType,
		patchData,
		metav1.PatchOptions{},
	)
	return err
}

// patchCRD patches a CRD to remove finalizers
func (e *Executor) patchCRD(ctx context.Context, name string) error {
	patchData := []byte(`{"metadata":{"finalizers":null}}`)
	_, err := e.client.ApiExtensions.ApiextensionsV1().CustomResourceDefinitions().Patch(
		ctx,
		name,
		k8stypes.MergePatchType,
		patchData,
		metav1.PatchOptions{},
	)
	return err
}

// patchDynamicResource patches a dynamic resource to remove finalizers
func (e *Executor) patchDynamicResource(ctx context.Context, target types.ResourceRef) error {
	// Parse the API version to get group and version
	gvr, err := e.resolveGVR(target)
	if err != nil {
		return fmt.Errorf("failed to resolve GVR: %w", err)
	}

	patchData := []byte(`{"metadata":{"finalizers":null}}`)
	_, err = e.client.Dynamic.Resource(gvr).Namespace(target.Namespace).Patch(
		ctx,
		target.Name,
		k8stypes.MergePatchType,
		patchData,
		metav1.PatchOptions{},
	)
	return err
}

// executeDelete deletes a resource
func (e *Executor) executeDelete(ctx context.Context, action types.Action) error {
	target := action.Target

	switch target.Kind {
	case "Namespace":
		return e.client.Clientset.CoreV1().Namespaces().Delete(ctx, target.Name, metav1.DeleteOptions{})
	case "CustomResourceDefinition":
		return e.client.ApiExtensions.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, target.Name, metav1.DeleteOptions{})
	default:
		gvr, err := e.resolveGVR(target)
		if err != nil {
			return fmt.Errorf("failed to resolve GVR: %w", err)
		}
		return e.client.Dynamic.Resource(gvr).Namespace(target.Namespace).Delete(ctx, target.Name, metav1.DeleteOptions{})
	}
}

// executeFinalize force-finalizes a namespace using the finalize subresource
func (e *Executor) executeFinalize(ctx context.Context, action types.Action) error {
	target := action.Target

	if target.Kind != "Namespace" {
		return fmt.Errorf("finalize action only supports Namespace, got: %s", target.Kind)
	}

	// Get the current namespace
	ns, err := e.client.Clientset.CoreV1().Namespaces().Get(ctx, target.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get namespace: %w", err)
	}

	// Clear finalizers
	ns.Spec.Finalizers = nil

	// Use the finalize subresource
	_, err = e.client.Clientset.CoreV1().Namespaces().Finalize(ctx, ns, metav1.UpdateOptions{})
	return err
}

// executeWait waits for a condition to be met
func (e *Executor) executeWait(ctx context.Context, action types.Action) error {
	// For now, wait is a no-op - can be implemented with watchers if needed
	return nil
}

// Verify checks if the post-condition for an action is met
func (e *Executor) Verify(ctx context.Context, action types.Action) (bool, error) {
	switch action.Type {
	case types.ActionInspect, types.ActionList, types.ActionWait:
		// These actions always succeed
		return true, nil

	case types.ActionPatch:
		// Verify finalizers are removed
		return e.verifyNoFinalizers(ctx, action.Target)

	case types.ActionDelete:
		// Verify resource is gone (404)
		return e.verifyDeleted(ctx, action.Target)

	case types.ActionFinalize:
		// Verify namespace is gone or no longer terminating
		return e.verifyNamespaceCleared(ctx, action.Target.Name)

	default:
		return false, fmt.Errorf("unknown action type for verification: %s", action.Type)
	}
}

// verifyNoFinalizers checks that a resource has no finalizers
func (e *Executor) verifyNoFinalizers(ctx context.Context, target types.ResourceRef) (bool, error) {
	switch target.Kind {
	case "Namespace":
		ns, err := e.client.Clientset.CoreV1().Namespaces().Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return len(ns.Finalizers) == 0, nil

	case "CustomResourceDefinition":
		crd, err := e.client.ApiExtensions.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return len(crd.Finalizers) == 0, nil

	default:
		gvr, err := e.resolveGVR(target)
		if err != nil {
			return false, err
		}
		obj, err := e.client.Dynamic.Resource(gvr).Namespace(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		finalizers, _, _ := unstructured.NestedStringSlice(obj.Object, "metadata", "finalizers")
		return len(finalizers) == 0, nil
	}
}

// verifyDeleted checks that a resource no longer exists
func (e *Executor) verifyDeleted(ctx context.Context, target types.ResourceRef) (bool, error) {
	switch target.Kind {
	case "Namespace":
		_, err := e.client.Clientset.CoreV1().Namespaces().Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return true, nil
			}
			return false, err
		}
		return false, nil

	case "CustomResourceDefinition":
		_, err := e.client.ApiExtensions.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return true, nil
			}
			return false, err
		}
		return false, nil

	default:
		gvr, err := e.resolveGVR(target)
		if err != nil {
			// If we can't resolve GVR, the resource type might be gone
			return true, nil
		}
		_, err = e.client.Dynamic.Resource(gvr).Namespace(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}
}

// verifyNamespaceCleared checks if a namespace is gone or no longer terminating
func (e *Executor) verifyNamespaceCleared(ctx context.Context, name string) (bool, error) {
	ns, err := e.client.Clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return true, nil // Namespace is gone
		}
		return false, err
	}

	// Check if still terminating
	return ns.Status.Phase != "Terminating", nil
}

// Snapshot captures the current state of a resource for audit logging
func (e *Executor) Snapshot(ctx context.Context, target types.ResourceRef) (interface{}, error) {
	switch target.Kind {
	case "Namespace":
		ns, err := e.client.Clientset.CoreV1().Namespaces().Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"finalizers":        ns.Finalizers,
			"deletionTimestamp": ns.DeletionTimestamp,
			"phase":             ns.Status.Phase,
		}, nil

	case "CustomResourceDefinition":
		crd, err := e.client.ApiExtensions.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"finalizers":        crd.Finalizers,
			"deletionTimestamp": crd.DeletionTimestamp,
		}, nil

	default:
		gvr, err := e.resolveGVR(target)
		if err != nil {
			return nil, err
		}
		obj, err := e.client.Dynamic.Resource(gvr).Namespace(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		finalizers, _, _ := unstructured.NestedStringSlice(obj.Object, "metadata", "finalizers")
		deletionTimestamp, _, _ := unstructured.NestedString(obj.Object, "metadata", "deletionTimestamp")
		return map[string]interface{}{
			"finalizers":        finalizers,
			"deletionTimestamp": deletionTimestamp,
		}, nil
	}
}

// resolveGVR resolves a ResourceRef to a GroupVersionResource
func (e *Executor) resolveGVR(target types.ResourceRef) (schema.GroupVersionResource, error) {
	// Parse API version
	gv := target.APIVersion
	if gv == "" {
		gv = "v1" // Default to core API
	}

	parts := strings.Split(gv, "/")
	var group, version string
	if len(parts) == 1 {
		group = ""
		version = parts[0]
	} else {
		group = parts[0]
		version = parts[1]
	}

	// Get resource name (plural, lowercase)
	resource := pluralize(target.Kind)

	return schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resource,
	}, nil
}

// pluralize returns the plural form of a Kubernetes kind
func pluralize(kind string) string {
	lower := strings.ToLower(kind)
	switch lower {
	case "endpoints":
		return "endpoints"
	case "ingress":
		return "ingresses"
	case "networkpolicy":
		return "networkpolicies"
	case "podsecuritypolicy":
		return "podsecuritypolicies"
	case "resourcequota":
		return "resourcequotas"
	case "limitrange":
		return "limitranges"
	default:
		if strings.HasSuffix(lower, "s") {
			return lower + "es"
		}
		if strings.HasSuffix(lower, "y") {
			return lower[:len(lower)-1] + "ies"
		}
		return lower + "s"
	}
}

// SnapshotJSON returns the snapshot as a JSON string for logging
func SnapshotJSON(snapshot interface{}) string {
	if snapshot == nil {
		return "null"
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return string(data)
}
