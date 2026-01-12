package detector

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/sozercan/unstuck/pkg/kube"
	"github.com/sozercan/unstuck/pkg/types"
)

// WebhookDetector detects webhook interference issues
type WebhookDetector struct {
	client *kube.Client
}

// NewWebhookDetector creates a new webhook detector
func NewWebhookDetector(client *kube.Client) *WebhookDetector {
	return &WebhookDetector{client: client}
}

// WebhookErrorPatterns contains patterns to match webhook errors
var WebhookErrorPatterns = []struct {
	Pattern *regexp.Regexp
	Type    string
}{
	{regexp.MustCompile(`admission webhook "([^"]+)" denied the request`), "webhook_denied"},
	{regexp.MustCompile(`failed calling webhook "([^"]+)"`), "webhook_failed"},
	{regexp.MustCompile(`context deadline exceeded`), "webhook_timeout"},
	{regexp.MustCompile(`connection refused`), "webhook_unavailable"},
	{regexp.MustCompile(`no endpoints available`), "webhook_no_endpoints"},
	{regexp.MustCompile(`service .* not found`), "webhook_service_missing"},
	{regexp.MustCompile(`Internal error occurred: failed calling webhook`), "webhook_internal_error"},
}

// WebhookReport contains the results of webhook detection
type WebhookReport struct {
	ValidatingWebhooks []types.WebhookInfo `json:"validatingWebhooks,omitempty"`
	MutatingWebhooks   []types.WebhookInfo `json:"mutatingWebhooks,omitempty"`
}

// HasIssues returns true if any webhooks are unhealthy
func (r *WebhookReport) HasIssues() bool {
	for _, w := range r.ValidatingWebhooks {
		if !w.Healthy {
			return true
		}
	}
	for _, w := range r.MutatingWebhooks {
		if !w.Healthy {
			return true
		}
	}
	return false
}

// AllWebhooks returns all webhooks combined
func (r *WebhookReport) AllWebhooks() []types.WebhookInfo {
	result := make([]types.WebhookInfo, 0, len(r.ValidatingWebhooks)+len(r.MutatingWebhooks))
	result = append(result, r.ValidatingWebhooks...)
	result = append(result, r.MutatingWebhooks...)
	return result
}

// UnhealthyWebhooks returns only unhealthy webhooks
func (r *WebhookReport) UnhealthyWebhooks() []types.WebhookInfo {
	var result []types.WebhookInfo
	for _, w := range r.AllWebhooks() {
		if !w.Healthy {
			result = append(result, w)
		}
	}
	return result
}

// DetectForGVR checks if any webhooks might interfere with operations on a GVR
func (d *WebhookDetector) DetectForGVR(ctx context.Context, gvr schema.GroupVersionResource) ([]types.WebhookInfo, error) {
	var webhookInfos []types.WebhookInfo

	vwcs, err := d.client.Clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, vwc := range vwcs.Items {
		for _, webhook := range vwc.Webhooks {
			if matchesGVR(webhook.Rules, gvr) {
				info := types.WebhookInfo{
					Name:        vwc.Name,
					WebhookName: webhook.Name,
					Type:        "validating",
					Healthy:     true,
				}
				if webhook.ClientConfig.Service != nil {
					info.ServiceRef = webhook.ClientConfig.Service.Namespace + "/" + webhook.ClientConfig.Service.Name
				}
				if webhook.FailurePolicy != nil {
					info.FailurePolicy = string(*webhook.FailurePolicy)
				}
				webhookInfos = append(webhookInfos, info)
			}
		}
	}

	mwcs, err := d.client.Clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, mwc := range mwcs.Items {
		for _, webhook := range mwc.Webhooks {
			if matchesMutatingGVR(webhook.Rules, gvr) {
				info := types.WebhookInfo{
					Name:        mwc.Name,
					WebhookName: webhook.Name,
					Type:        "mutating",
					Healthy:     true,
				}
				if webhook.ClientConfig.Service != nil {
					info.ServiceRef = webhook.ClientConfig.Service.Namespace + "/" + webhook.ClientConfig.Service.Name
				}
				if webhook.FailurePolicy != nil {
					info.FailurePolicy = string(*webhook.FailurePolicy)
				}
				webhookInfos = append(webhookInfos, info)
			}
		}
	}

	return webhookInfos, nil
}

// IsWebhookError checks if an error is caused by a webhook
func IsWebhookError(err error) (bool, string) {
	if err == nil {
		return false, ""
	}
	msg := err.Error()
	for _, pattern := range WebhookErrorPatterns {
		if pattern.Pattern.MatchString(msg) {
			return true, pattern.Type
		}
	}
	return false, ""
}

// ExtractWebhookNameFromError attempts to extract the webhook name from an error message
func ExtractWebhookNameFromError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()

	pattern := regexp.MustCompile(`admission webhook "([^"]+)"`)
	matches := pattern.FindStringSubmatch(msg)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func matchesGVR(rules []admissionv1.RuleWithOperations, gvr schema.GroupVersionResource) bool {
	for _, rule := range rules {
		if matchesRule(rule.Rule, gvr) {
			return true
		}
	}
	return false
}

func matchesMutatingGVR(rules []admissionv1.RuleWithOperations, gvr schema.GroupVersionResource) bool {
	return matchesGVR(rules, gvr)
}

func matchesRule(rule admissionv1.Rule, gvr schema.GroupVersionResource) bool {
	groupMatch := false
	for _, g := range rule.APIGroups {
		if g == "*" || g == gvr.Group {
			groupMatch = true
			break
		}
	}
	if !groupMatch {
		return false
	}

	versionMatch := false
	for _, v := range rule.APIVersions {
		if v == "*" || v == gvr.Version {
			versionMatch = true
			break
		}
	}
	if !versionMatch {
		return false
	}

	for _, r := range rule.Resources {
		if r == "*" || r == gvr.Resource || strings.HasPrefix(r, gvr.Resource+"/") {
			return true
		}
	}

	return false
}

// DetectWithHealthCheck detects webhooks for a GVR and checks their health
func (d *WebhookDetector) DetectWithHealthCheck(ctx context.Context, gvr schema.GroupVersionResource) (*WebhookReport, error) {
	report := &WebhookReport{}

	// Check validating webhooks
	vwcs, err := d.client.Clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list validating webhooks: %w", err)
	}

	for _, vwc := range vwcs.Items {
		for _, webhook := range vwc.Webhooks {
			if matchesGVR(webhook.Rules, gvr) {
				info := d.buildWebhookInfoWithHealth(ctx, vwc.Name, webhook.Name, "validating", webhook.ClientConfig, webhook.FailurePolicy)
				report.ValidatingWebhooks = append(report.ValidatingWebhooks, info)
			}
		}
	}

	// Check mutating webhooks
	mwcs, err := d.client.Clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list mutating webhooks: %w", err)
	}

	for _, mwc := range mwcs.Items {
		for _, webhook := range mwc.Webhooks {
			if matchesGVR(webhook.Rules, gvr) {
				info := d.buildWebhookInfoWithHealth(ctx, mwc.Name, webhook.Name, "mutating", webhook.ClientConfig, webhook.FailurePolicy)
				report.MutatingWebhooks = append(report.MutatingWebhooks, info)
			}
		}
	}

	return report, nil
}

// buildWebhookInfoWithHealth creates a WebhookInfo with health check
func (d *WebhookDetector) buildWebhookInfoWithHealth(ctx context.Context, configName, webhookName, webhookType string, clientConfig admissionv1.WebhookClientConfig, failurePolicy *admissionv1.FailurePolicyType) types.WebhookInfo {
	info := types.WebhookInfo{
		Name:        configName,
		WebhookName: webhookName,
		Type:        webhookType,
		Healthy:     true, // Assume healthy until proven otherwise
	}

	if failurePolicy != nil {
		info.FailurePolicy = string(*failurePolicy)
	}

	// Check service health if webhook uses a service reference
	if clientConfig.Service != nil {
		svcRef := fmt.Sprintf("%s/%s", clientConfig.Service.Namespace, clientConfig.Service.Name)
		info.ServiceRef = svcRef

		healthy, errMsg := d.checkServiceHealth(ctx, clientConfig.Service.Namespace, clientConfig.Service.Name)
		info.Healthy = healthy
		if errMsg != "" {
			info.Error = errMsg
		}
	}

	return info
}

// checkServiceHealth verifies that a webhook service has healthy endpoints
func (d *WebhookDetector) checkServiceHealth(ctx context.Context, namespace, name string) (bool, string) {
	// Check if service exists
	_, err := d.client.Clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return false, fmt.Sprintf("service %s/%s not found", namespace, name)
		}
		return false, fmt.Sprintf("failed to get service: %v", err)
	}

	// Check if endpoints exist
	endpoints, err := d.client.Clientset.CoreV1().Endpoints(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return false, fmt.Sprintf("endpoints %s/%s not found", namespace, name)
		}
		return false, fmt.Sprintf("failed to get endpoints: %v", err)
	}

	// Check if there are ready addresses
	readyCount := countReadyAddresses(endpoints)
	if readyCount == 0 {
		return false, "no ready endpoints available"
	}

	return true, ""
}

// countReadyAddresses counts ready addresses in endpoints
func countReadyAddresses(endpoints *corev1.Endpoints) int {
	count := 0
	for _, subset := range endpoints.Subsets {
		count += len(subset.Addresses)
	}
	return count
}

// GenerateWebhookGuidance generates user-friendly guidance for resolving webhook issues
func GenerateWebhookGuidance(webhook types.WebhookInfo) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("⚠️  Webhook %q is blocking operations\n\n", webhook.Name))

	if webhook.Error != "" {
		sb.WriteString(fmt.Sprintf("Error: %s\n\n", webhook.Error))
	}

	sb.WriteString("To proceed, you must manually resolve the webhook:\n\n")

	if webhook.ServiceRef != "" {
		parts := strings.Split(webhook.ServiceRef, "/")
		if len(parts) == 2 {
			ns, name := parts[0], parts[1]

			sb.WriteString("  Option 1: Restore the webhook service\n")
			sb.WriteString(fmt.Sprintf("    kubectl rollout restart deployment -n %s -l app=%s\n\n", ns, name))

			sb.WriteString("  Option 2: Check webhook service health\n")
			sb.WriteString(fmt.Sprintf("    kubectl get endpoints %s -n %s\n", name, ns))
			sb.WriteString(fmt.Sprintf("    kubectl describe svc %s -n %s\n\n", name, ns))
		}
	}

	sb.WriteString("  Option 3: Temporarily disable the webhook (use with extreme caution!)\n")
	if webhook.Type == "validating" {
		sb.WriteString(fmt.Sprintf("    kubectl delete validatingwebhookconfiguration %s\n\n", webhook.Name))
	} else {
		sb.WriteString(fmt.Sprintf("    kubectl delete mutatingwebhookconfiguration %s\n\n", webhook.Name))
	}

	if webhook.FailurePolicy == "Fail" {
		sb.WriteString("  ⚠️  NOTE: This webhook has FailurePolicy=Fail, meaning operations will fail if the webhook is unavailable.\n")
	} else if webhook.FailurePolicy == "Ignore" {
		sb.WriteString("  ℹ️  NOTE: This webhook has FailurePolicy=Ignore, meaning operations may succeed even if the webhook is unavailable.\n")
	}

	return sb.String()
}

// WebhookErrorTypeDescription returns a human-readable description of a webhook error type
func WebhookErrorTypeDescription(errorType string) string {
	switch errorType {
	case "webhook_denied":
		return "The webhook actively denied the request"
	case "webhook_failed":
		return "The webhook call failed"
	case "webhook_timeout":
		return "The webhook call timed out"
	case "webhook_unavailable":
		return "The webhook service is unavailable (connection refused)"
	case "webhook_no_endpoints":
		return "The webhook service has no available endpoints"
	case "webhook_service_missing":
		return "The webhook service does not exist"
	case "webhook_internal_error":
		return "An internal error occurred calling the webhook"
	default:
		return "Unknown webhook error"
	}
}
