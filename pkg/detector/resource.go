package detector

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/sozercan/unstuck/pkg/kube"
	"github.com/sozercan/unstuck/pkg/types"
)

// ResourceDetector detects issues with stuck generic resources
type ResourceDetector struct {
	client *kube.Client
}

// NewResourceDetector creates a new resource detector
func NewResourceDetector(client *kube.Client) *ResourceDetector {
	return &ResourceDetector{client: client}
}

// Detect analyzes a specific resource and returns a diagnosis report
func (d *ResourceDetector) Detect(ctx context.Context, resourceType, name, namespace string) (*types.DiagnosisReport, error) {
	// Resolve the resource type to a GVR
	gvr, kind, err := d.resolveResourceType(ctx, resourceType)
	if err != nil {
		return nil, err
	}

	// Fetch the resource
	var obj metav1.Object
	var apiVersion string

	if namespace != "" {
		unstructured, err := d.client.Dynamic.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get %s %q in namespace %q: %w", resourceType, name, namespace, err)
		}
		obj = unstructured
		apiVersion = unstructured.GetAPIVersion()
	} else {
		unstructured, err := d.client.Dynamic.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get %s %q: %w", resourceType, name, err)
		}
		obj = unstructured
		apiVersion = unstructured.GetAPIVersion()
	}

	report := &types.DiagnosisReport{
		Target: types.ResourceRef{
			Kind:       kind,
			APIVersion: apiVersion,
			Namespace:  namespace,
			Name:       name,
		},
		TargetType:  types.TargetTypeResource,
		Finalizers:  obj.GetFinalizers(),
		DiagnosedAt: time.Now(),
	}

	// Check deletion timestamp
	if obj.GetDeletionTimestamp() == nil {
		report.Status = "Active"
		report.Recommendations = []string{
			fmt.Sprintf("%s %q is not in Terminating state. No remediation needed.", kind, name),
		}
		return report, nil
	}

	report.Status = "Terminating"
	t := obj.GetDeletionTimestamp().Time
	report.DeletionTimestamp = &t
	report.TerminatingFor = formatDuration(time.Since(t))

	// Analyze root cause
	if len(obj.GetFinalizers()) > 0 {
		report.RootCause = fmt.Sprintf("Resource has finalizers: %v", obj.GetFinalizers())
	} else {
		report.RootCause = "Resource is terminating but has no finalizers (may be waiting for dependents)"
	}

	// Add self as a blocker for consistency
	report.Blockers = []types.Blocker{
		{
			ResourceRef: types.ResourceRef{
				Kind:       kind,
				APIVersion: apiVersion,
				Namespace:  namespace,
				Name:       name,
			},
			Finalizers:        obj.GetFinalizers(),
			DeletionTimestamp: obj.GetDeletionTimestamp(),
			OwnerReferences:   obj.GetOwnerReferences(),
			Age:               time.Since(t),
		},
	}

	// Build recommendations
	report.Recommendations = d.buildRecommendations(report, kind, name, namespace)

	return report, nil
}

// resolveResourceType resolves a resource type string to a GVR
func (d *ResourceDetector) resolveResourceType(ctx context.Context, resourceType string) (schema.GroupVersionResource, string, error) {
	// Get all API resources
	_, apiResourceLists, err := d.client.Discovery.ServerGroupsAndResources()
	if err != nil && !isPartialDiscoveryError(err) {
		return schema.GroupVersionResource{}, "", fmt.Errorf("failed to discover API resources: %w", err)
	}

	resourceType = strings.ToLower(resourceType)

	for _, resourceList := range apiResourceLists {
		gv, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil {
			continue
		}

		for _, apiResource := range resourceList.APIResources {
			// Skip subresources
			if strings.Contains(apiResource.Name, "/") {
				continue
			}

			// Match by name, singular name, or short names
			if strings.ToLower(apiResource.Name) == resourceType ||
				strings.ToLower(apiResource.SingularName) == resourceType ||
				containsIgnoreCase(apiResource.ShortNames, resourceType) {

				return schema.GroupVersionResource{
					Group:    gv.Group,
					Version:  gv.Version,
					Resource: apiResource.Name,
				}, apiResource.Kind, nil
			}
		}
	}

	return schema.GroupVersionResource{}, "", fmt.Errorf("unknown resource type %q", resourceType)
}

// buildRecommendations generates recommendations for resource issues
func (d *ResourceDetector) buildRecommendations(report *types.DiagnosisReport, kind, name, namespace string) []string {
	var recs []string

	if len(report.Finalizers) > 0 {
		if namespace != "" {
			recs = append(recs, fmt.Sprintf(
				"Use `terminator plan %s %s -n %s` to generate remediation steps.",
				strings.ToLower(kind), name, namespace,
			))
		} else {
			recs = append(recs, fmt.Sprintf(
				"Use `terminator plan %s %s` to generate remediation steps.",
				strings.ToLower(kind), name,
			))
		}
	} else {
		recs = append(recs, "Resource has no finalizers but is still terminating. Check for dependent resources or controller issues.")
	}

	return recs
}

// containsIgnoreCase checks if a string slice contains a value (case-insensitive)
func containsIgnoreCase(slice []string, val string) bool {
	val = strings.ToLower(val)
	for _, s := range slice {
		if strings.ToLower(s) == val {
			return true
		}
	}
	return false
}
