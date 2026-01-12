package detector

import (
	"context"
	"fmt"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/sozercan/unstuck/pkg/kube"
	"github.com/sozercan/unstuck/pkg/types"
)

// CRDDetector detects issues with stuck CRDs
type CRDDetector struct {
	client *kube.Client
}

// NewCRDDetector creates a new CRD detector
func NewCRDDetector(client *kube.Client) *CRDDetector {
	return &CRDDetector{client: client}
}

// Detect analyzes a CRD and returns a diagnosis report
func (d *CRDDetector) Detect(ctx context.Context, name string) (*types.DiagnosisReport, error) {
	crd, err := d.client.ApiExtensions.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("CRD %q not found", name)
		}
		return nil, fmt.Errorf("failed to get CRD: %w", err)
	}

	report := &types.DiagnosisReport{
		Target: types.ResourceRef{
			Kind:       "CustomResourceDefinition",
			APIVersion: "apiextensions.k8s.io/v1",
			Name:       name,
		},
		TargetType:  types.TargetTypeCRD,
		Finalizers:  crd.Finalizers,
		DiagnosedAt: time.Now(),
	}

	if crd.DeletionTimestamp == nil {
		report.Status = "Active"
		report.Recommendations = []string{
			fmt.Sprintf("CRD %q is not in Terminating state. No remediation needed.", name),
		}
		return report, nil
	}

	report.Status = "Terminating"
	t := crd.DeletionTimestamp.Time
	report.DeletionTimestamp = &t
	report.TerminatingFor = formatDuration(time.Since(t))

	report.RootCause = d.analyzeRootCause(crd)

	instanceCount, blockers, instancesByNS, err := d.countInstances(ctx, crd)
	if err != nil {
		report.DiscoveryFailures = append(report.DiscoveryFailures, types.DiscoveryFailure{
			GroupVersion: crd.Spec.Group + "/" + getStoredVersion(crd),
			Resource:     crd.Spec.Names.Plural,
			Error:        err.Error(),
		})
	}
	report.InstanceCount = instanceCount
	report.InstancesByNS = instancesByNS
	report.Blockers = blockers

	report.Recommendations = d.buildRecommendations(report, crd)

	return report, nil
}

func (d *CRDDetector) countInstances(ctx context.Context, crd *apiextensionsv1.CustomResourceDefinition) (int, []types.Blocker, map[string]int, error) {
	gvr := schema.GroupVersionResource{
		Group:    crd.Spec.Group,
		Version:  getStoredVersion(crd),
		Resource: crd.Spec.Names.Plural,
	}

	var blockers []types.Blocker
	instancesByNS := make(map[string]int)

	if crd.Spec.Scope == apiextensionsv1.NamespaceScoped {
		unstructuredList, listErr := d.client.Dynamic.Resource(gvr).Namespace("").List(ctx, metav1.ListOptions{})
		if listErr != nil {
			return 0, nil, nil, listErr
		}

		for _, item := range unstructuredList.Items {
			ns := item.GetNamespace()
			instancesByNS[ns]++

			blocker := types.Blocker{
				ResourceRef: types.ResourceRef{
					Kind:       item.GetKind(),
					APIVersion: item.GetAPIVersion(),
					Namespace:  ns,
					Name:       item.GetName(),
				},
				Finalizers:        item.GetFinalizers(),
				DeletionTimestamp: item.GetDeletionTimestamp(),
				OwnerReferences:   item.GetOwnerReferences(),
			}
			if item.GetDeletionTimestamp() != nil {
				blocker.Age = time.Since(item.GetDeletionTimestamp().Time)
			}
			blockers = append(blockers, blocker)
		}

		return len(unstructuredList.Items), blockers, instancesByNS, nil
	}

	unstructuredList, err := d.client.Dynamic.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, nil, nil, err
	}

	for _, item := range unstructuredList.Items {
		blocker := types.Blocker{
			ResourceRef: types.ResourceRef{
				Kind:       item.GetKind(),
				APIVersion: item.GetAPIVersion(),
				Name:       item.GetName(),
			},
			Finalizers:        item.GetFinalizers(),
			DeletionTimestamp: item.GetDeletionTimestamp(),
			OwnerReferences:   item.GetOwnerReferences(),
		}
		if item.GetDeletionTimestamp() != nil {
			blocker.Age = time.Since(item.GetDeletionTimestamp().Time)
		}
		blockers = append(blockers, blocker)
	}

	return len(unstructuredList.Items), blockers, instancesByNS, nil
}

func (d *CRDDetector) analyzeRootCause(crd *apiextensionsv1.CustomResourceDefinition) string {
	for _, f := range crd.Finalizers {
		if f == "customresourcecleanup.apiextensions.k8s.io" {
			return "CRD cleanup finalizer waiting for instance deletion"
		}
	}
	if len(crd.Finalizers) > 0 {
		return fmt.Sprintf("CRD has custom finalizers: %v", crd.Finalizers)
	}
	return "Unknown - CRD may be waiting for API server processing"
}

func (d *CRDDetector) buildRecommendations(report *types.DiagnosisReport, crd *apiextensionsv1.CustomResourceDefinition) []string {
	var recs []string

	if report.InstanceCount > 0 {
		recs = append(recs, fmt.Sprintf(
			"%d CR instances remain across %d namespaces. Remove finalizers from instances first, then CRD will auto-delete.",
			report.InstanceCount,
			len(report.InstancesByNS),
		))
		recs = append(recs, fmt.Sprintf(
			"Use `terminator plan crd %s` to generate steps.",
			crd.Name,
		))
	} else if hasCleanupFinalizer(crd) {
		recs = append(recs, "CRD has cleanup finalizer but no instances found. May need force removal.")
		recs = append(recs, fmt.Sprintf(
			"Use `terminator plan crd %s --max-escalation=3 --allow-force`",
			crd.Name,
		))
	}

	return recs
}

func getStoredVersion(crd *apiextensionsv1.CustomResourceDefinition) string {
	for _, v := range crd.Spec.Versions {
		if v.Storage {
			return v.Name
		}
	}
	if len(crd.Spec.Versions) > 0 {
		return crd.Spec.Versions[0].Name
	}
	return "v1"
}

func hasCleanupFinalizer(crd *apiextensionsv1.CustomResourceDefinition) bool {
	for _, f := range crd.Finalizers {
		if f == "customresourcecleanup.apiextensions.k8s.io" {
			return true
		}
	}
	return false
}
