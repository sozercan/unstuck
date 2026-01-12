package detector

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/sozercan/unstuck/pkg/kube"
	"github.com/sozercan/unstuck/pkg/types"
)

// NamespaceDetector detects issues with stuck namespaces
type NamespaceDetector struct {
	client *kube.Client
}

// NewNamespaceDetector creates a new namespace detector
func NewNamespaceDetector(client *kube.Client) *NamespaceDetector {
	return &NamespaceDetector{client: client}
}

// Detect analyzes a namespace and returns a diagnosis report
func (d *NamespaceDetector) Detect(ctx context.Context, name string) (*types.DiagnosisReport, error) {
	ns, err := d.client.Clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("namespace %q not found", name)
		}
		return nil, fmt.Errorf("failed to get namespace: %w", err)
	}

	report := &types.DiagnosisReport{
		Target: types.ResourceRef{
			Kind:       "Namespace",
			APIVersion: "v1",
			Name:       name,
		},
		TargetType:  types.TargetTypeNamespace,
		Status:      string(ns.Status.Phase),
		Finalizers:  ns.Finalizers,
		DiagnosedAt: time.Now(),
	}

	if ns.Status.Phase != corev1.NamespaceTerminating {
		report.Recommendations = []string{
			fmt.Sprintf("Namespace %q is not in Terminating state. No remediation needed.", name),
		}
		return report, nil
	}

	if ns.DeletionTimestamp != nil {
		t := ns.DeletionTimestamp.Time
		report.DeletionTimestamp = &t
		report.TerminatingFor = formatDuration(time.Since(t))
	}

	report.Conditions = parseNamespaceConditions(ns.Status.Conditions)
	report.RootCause, report.DiscoveryFailures = analyzeConditions(ns.Status.Conditions)

	blockers, err := d.enumerateResources(ctx, name)
	if err != nil {
		report.DiscoveryFailures = append(report.DiscoveryFailures, types.DiscoveryFailure{
			GroupVersion: "unknown",
			Error:        err.Error(),
		})
	}
	report.Blockers = blockers

	report.Recommendations = d.buildRecommendations(report)

	return report, nil
}

func (d *NamespaceDetector) enumerateResources(ctx context.Context, namespace string) ([]types.Blocker, error) {
	var blockers []types.Blocker

	_, apiResourceLists, err := d.client.Discovery.ServerGroupsAndResources()
	if err != nil {
		if !isPartialDiscoveryError(err) {
			return nil, err
		}
	}

	for _, resourceList := range apiResourceLists {
		gv, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil {
			continue
		}

		for _, apiResource := range resourceList.APIResources {
			if !apiResource.Namespaced {
				continue
			}

			if !containsVerb(apiResource.Verbs, "list") {
				continue
			}

			if strings.Contains(apiResource.Name, "/") {
				continue
			}

			gvr := schema.GroupVersionResource{
				Group:    gv.Group,
				Version:  gv.Version,
				Resource: apiResource.Name,
			}

			list, err := d.client.Dynamic.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				continue
			}

			for _, item := range list.Items {
				finalizers := item.GetFinalizers()
				deletionTimestamp := item.GetDeletionTimestamp()

				if len(finalizers) > 0 || deletionTimestamp != nil {
					blocker := types.Blocker{
						ResourceRef: types.ResourceRef{
							Kind:       item.GetKind(),
							APIVersion: item.GetAPIVersion(),
							Namespace:  item.GetNamespace(),
							Name:       item.GetName(),
						},
						Finalizers:        finalizers,
						DeletionTimestamp: deletionTimestamp,
						OwnerReferences:   item.GetOwnerReferences(),
					}
					if deletionTimestamp != nil {
						blocker.Age = time.Since(deletionTimestamp.Time)
					}
					blockers = append(blockers, blocker)
				}
			}
		}
	}

	return blockers, nil
}

func (d *NamespaceDetector) buildRecommendations(report *types.DiagnosisReport) []string {
	var recs []string

	if report.HasDiscoveryFailures() {
		recs = append(recs, "Discovery failures detected. CRDs may have been deleted. Use `terminator plan` with --max-escalation=4 --allow-force")
	}

	if report.HasBlockers() {
		stuckCount := 0
		for _, b := range report.Blockers {
			if b.IsTerminating() {
				stuckCount++
			}
		}
		if stuckCount > 0 {
			recs = append(recs, fmt.Sprintf("%d resources are stuck in Terminating. Use `terminator plan namespace %s` to generate remediation steps.", stuckCount, report.Target.Name))
		} else {
			recs = append(recs, fmt.Sprintf("%d resources have finalizers. Use `terminator plan namespace %s` to see remediation options.", len(report.Blockers), report.Target.Name))
		}
	}

	if len(recs) == 0 {
		recs = append(recs, "Unable to determine specific blockers. The namespace finalizer may need force removal.")
	}

	return recs
}

func parseNamespaceConditions(conditions []corev1.NamespaceCondition) []types.ConditionSummary {
	var summaries []types.ConditionSummary
	for _, c := range conditions {
		summaries = append(summaries, types.ConditionSummary{
			Type:    string(c.Type),
			Status:  string(c.Status),
			Reason:  c.Reason,
			Message: c.Message,
		})
	}
	return summaries
}

func analyzeConditions(conditions []corev1.NamespaceCondition) (string, []types.DiscoveryFailure) {
	var rootCause string
	var failures []types.DiscoveryFailure

	for _, c := range conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}

		switch c.Type {
		case corev1.NamespaceDeletionDiscoveryFailure:
			rootCause = "Discovery failures - CRD may have been deleted"
			gvrs := parseDiscoveryFailureMessage(c.Message)
			for _, gvr := range gvrs {
				failures = append(failures, types.DiscoveryFailure{
					GroupVersion: gvr,
					Error:        "API group not found (CRD likely deleted)",
				})
			}

		case corev1.NamespaceFinalizersRemaining:
			if rootCause == "" {
				rootCause = "CR instances stuck with unsatisfied finalizers"
			}

		case corev1.NamespaceContentRemaining:
			if rootCause == "" {
				rootCause = "Resources remaining in namespace"
			}
		}
	}

	if rootCause == "" {
		rootCause = "Namespace finalizer blocking deletion"
	}

	return rootCause, failures
}

func parseDiscoveryFailureMessage(message string) []string {
	pattern := regexp.MustCompile(`([a-zA-Z0-9.-]+/v[a-zA-Z0-9]+)`)
	matches := pattern.FindAllStringSubmatch(message, -1)

	var gvrs []string
	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) > 1 && !seen[m[1]] {
			gvrs = append(gvrs, m[1])
			seen[m[1]] = true
		}
	}
	return gvrs
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func containsVerb(verbs []string, verb string) bool {
	for _, v := range verbs {
		if v == verb {
			return true
		}
	}
	return false
}

func isPartialDiscoveryError(err error) bool {
	return strings.Contains(err.Error(), "unable to retrieve the complete list")
}
