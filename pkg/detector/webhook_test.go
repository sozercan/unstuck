package detector

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/sozercan/unstuck/pkg/kube"
	"github.com/sozercan/unstuck/pkg/types"
)

func TestNewWebhookDetector(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	client := &kube.Client{Clientset: clientset}

	detector := NewWebhookDetector(client)
	assert.NotNil(t, detector)
	assert.Equal(t, client, detector.client)
}

func TestIsWebhookError(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedBool   bool
		expectedType   string
	}{
		{
			name:           "webhook denied",
			err:            errors.New("admission webhook \"validate.cert-manager.io\" denied the request"),
			expectedBool:   true,
			expectedType:   "webhook_denied",
		},
		{
			name:           "webhook failed",
			err:            errors.New("failed calling webhook \"validate.cert-manager.io\""),
			expectedBool:   true,
			expectedType:   "webhook_failed",
		},
		{
			name:           "connection refused",
			err:            errors.New("Post https://webhook.svc:443: connection refused"),
			expectedBool:   true,
			expectedType:   "webhook_unavailable",
		},
		{
			name:           "not webhook error",
			err:            errors.New("resource not found"),
			expectedBool:   false,
			expectedType:   "",
		},
		{
			name:           "nil error",
			err:            nil,
			expectedBool:   false,
			expectedType:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isWebhook, webhookType := IsWebhookError(tt.err)
			assert.Equal(t, tt.expectedBool, isWebhook)
			assert.Equal(t, tt.expectedType, webhookType)
		})
	}
}

func TestExtractWebhookNameFromError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "extract from admission webhook pattern",
			err:      errors.New("admission webhook \"validate.cert-manager.io\" denied the request"),
			expected: "validate.cert-manager.io",
		},
		{
			name:     "no webhook name",
			err:      errors.New("some random error"),
			expected: "",
		},
		{
			name:     "nil error",
			err:      nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractWebhookNameFromError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchesRule(t *testing.T) {
	tests := []struct {
		name        string
		rule        admissionv1.Rule
		gvr         schema.GroupVersionResource
		expectMatch bool
	}{
		{
			name: "exact match",
			rule: admissionv1.Rule{
				APIGroups:   []string{"apps"},
				APIVersions: []string{"v1"},
				Resources:   []string{"deployments"},
			},
			gvr:         schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			expectMatch: true,
		},
		{
			name: "wildcard groups",
			rule: admissionv1.Rule{
				APIGroups:   []string{"*"},
				APIVersions: []string{"v1"},
				Resources:   []string{"pods"},
			},
			gvr:         schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			expectMatch: true,
		},
		{
			name: "wildcard versions",
			rule: admissionv1.Rule{
				APIGroups:   []string{""},
				APIVersions: []string{"*"},
				Resources:   []string{"pods"},
			},
			gvr:         schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			expectMatch: true,
		},
		{
			name: "wildcard resources",
			rule: admissionv1.Rule{
				APIGroups:   []string{"apps"},
				APIVersions: []string{"v1"},
				Resources:   []string{"*"},
			},
			gvr:         schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			expectMatch: true,
		},
		{
			name: "no group match",
			rule: admissionv1.Rule{
				APIGroups:   []string{"batch"},
				APIVersions: []string{"v1"},
				Resources:   []string{"jobs"},
			},
			gvr:         schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "jobs"},
			expectMatch: false,
		},
		{
			name: "no version match",
			rule: admissionv1.Rule{
				APIGroups:   []string{"apps"},
				APIVersions: []string{"v1beta1"},
				Resources:   []string{"deployments"},
			},
			gvr:         schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			expectMatch: false,
		},
		{
			name: "no resource match",
			rule: admissionv1.Rule{
				APIGroups:   []string{"apps"},
				APIVersions: []string{"v1"},
				Resources:   []string{"statefulsets"},
			},
			gvr:         schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			expectMatch: false,
		},
		{
			name: "subresource match",
			rule: admissionv1.Rule{
				APIGroups:   []string{""},
				APIVersions: []string{"v1"},
				Resources:   []string{"pods/status"},
			},
			gvr:         schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			expectMatch: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := matchesRule(tc.rule, tc.gvr)
			assert.Equal(t, tc.expectMatch, result)
		})
	}
}

func TestWebhookReport_HasIssues(t *testing.T) {
	tests := []struct {
		name      string
		report    *WebhookReport
		expectHas bool
	}{
		{
			name:      "empty report",
			report:    &WebhookReport{},
			expectHas: false,
		},
		{
			name: "all healthy",
			report: &WebhookReport{
				ValidatingWebhooks: []types.WebhookInfo{{Name: "w1", Healthy: true}},
				MutatingWebhooks:   []types.WebhookInfo{{Name: "w2", Healthy: true}},
			},
			expectHas: false,
		},
		{
			name: "unhealthy validating",
			report: &WebhookReport{
				ValidatingWebhooks: []types.WebhookInfo{{Name: "w1", Healthy: false}},
			},
			expectHas: true,
		},
		{
			name: "unhealthy mutating",
			report: &WebhookReport{
				MutatingWebhooks: []types.WebhookInfo{{Name: "w1", Healthy: false}},
			},
			expectHas: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expectHas, tc.report.HasIssues())
		})
	}
}

func TestWebhookReport_AllWebhooks(t *testing.T) {
	report := &WebhookReport{
		ValidatingWebhooks: []types.WebhookInfo{{Name: "v1"}, {Name: "v2"}},
		MutatingWebhooks:   []types.WebhookInfo{{Name: "m1"}},
	}

	all := report.AllWebhooks()
	assert.Len(t, all, 3)
}

func TestWebhookReport_UnhealthyWebhooks(t *testing.T) {
	report := &WebhookReport{
		ValidatingWebhooks: []types.WebhookInfo{
			{Name: "v1", Healthy: true},
			{Name: "v2", Healthy: false},
		},
		MutatingWebhooks: []types.WebhookInfo{
			{Name: "m1", Healthy: false},
		},
	}

	unhealthy := report.UnhealthyWebhooks()
	assert.Len(t, unhealthy, 2)
}

func TestDetectForGVR(t *testing.T) {
	failPolicy := admissionv1.Fail

	vwc := &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "test-validating"},
		Webhooks: []admissionv1.ValidatingWebhook{
			{
				Name: "validate.example.com",
				Rules: []admissionv1.RuleWithOperations{
					{
						Rule: admissionv1.Rule{
							APIGroups:   []string{"apps"},
							APIVersions: []string{"v1"},
							Resources:   []string{"deployments"},
						},
					},
				},
				ClientConfig: admissionv1.WebhookClientConfig{
					Service: &admissionv1.ServiceReference{
						Namespace: "default",
						Name:      "webhook-svc",
					},
				},
				FailurePolicy: &failPolicy,
			},
		},
	}

	mwc := &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mutating"},
		Webhooks: []admissionv1.MutatingWebhook{
			{
				Name: "mutate.example.com",
				Rules: []admissionv1.RuleWithOperations{
					{
						Rule: admissionv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
						},
					},
				},
				ClientConfig: admissionv1.WebhookClientConfig{
					URL: stringPtr("https://webhook.example.com"),
				},
			},
		},
	}

	clientset := fake.NewSimpleClientset(vwc, mwc)
	client := &kube.Client{Clientset: clientset}
	detector := NewWebhookDetector(client)

	ctx := context.Background()

	// Test matching deployments
	webhooks, err := detector.DetectForGVR(ctx, schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	})
	assert.NoError(t, err)
	assert.Len(t, webhooks, 1)
	assert.Equal(t, "test-validating", webhooks[0].Name)
	assert.Equal(t, "default/webhook-svc", webhooks[0].ServiceRef)
	assert.Equal(t, "Fail", webhooks[0].FailurePolicy)

	// Test matching pods
	webhooks, err = detector.DetectForGVR(ctx, schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "pods",
	})
	assert.NoError(t, err)
	assert.Len(t, webhooks, 1)
	assert.Equal(t, "mutating", webhooks[0].Type)

	// Test no matches
	webhooks, err = detector.DetectForGVR(ctx, schema.GroupVersionResource{
		Group:    "batch",
		Version:  "v1",
		Resource: "jobs",
	})
	assert.NoError(t, err)
	assert.Len(t, webhooks, 0)
}

func TestDetectWithHealthCheck(t *testing.T) {
	failPolicy := admissionv1.Fail

	vwc := &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "test-validating"},
		Webhooks: []admissionv1.ValidatingWebhook{
			{
				Name: "validate.example.com",
				Rules: []admissionv1.RuleWithOperations{
					{
						Rule: admissionv1.Rule{
							APIGroups:   []string{"apps"},
							APIVersions: []string{"v1"},
							Resources:   []string{"deployments"},
						},
					},
				},
				ClientConfig: admissionv1.WebhookClientConfig{
					Service: &admissionv1.ServiceReference{
						Namespace: "default",
						Name:      "healthy-svc",
					},
				},
				FailurePolicy: &failPolicy,
			},
		},
	}

	// Healthy service with endpoints
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "healthy-svc",
			Namespace: "default",
		},
	}
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "healthy-svc",
			Namespace: "default",
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{IP: "10.0.0.1"},
				},
			},
		},
	}

	clientset := fake.NewSimpleClientset(vwc, svc, endpoints)
	client := &kube.Client{Clientset: clientset}
	detector := NewWebhookDetector(client)

	ctx := context.Background()

	report, err := detector.DetectWithHealthCheck(ctx, schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	})
	assert.NoError(t, err)
	assert.Len(t, report.ValidatingWebhooks, 1)
	assert.True(t, report.ValidatingWebhooks[0].Healthy)
	assert.False(t, report.HasIssues())
}

func TestDetectWithHealthCheck_UnhealthyService(t *testing.T) {
	failPolicy := admissionv1.Fail

	vwc := &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "unhealthy-validating"},
		Webhooks: []admissionv1.ValidatingWebhook{
			{
				Name: "validate.unhealthy.com",
				Rules: []admissionv1.RuleWithOperations{
					{
						Rule: admissionv1.Rule{
							APIGroups:   []string{"apps"},
							APIVersions: []string{"v1"},
							Resources:   []string{"deployments"},
						},
					},
				},
				ClientConfig: admissionv1.WebhookClientConfig{
					Service: &admissionv1.ServiceReference{
						Namespace: "default",
						Name:      "missing-svc",
					},
				},
				FailurePolicy: &failPolicy,
			},
		},
	}

	// No service or endpoints created - simulates missing service
	clientset := fake.NewSimpleClientset(vwc)
	client := &kube.Client{Clientset: clientset}
	detector := NewWebhookDetector(client)

	ctx := context.Background()

	report, err := detector.DetectWithHealthCheck(ctx, schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	})
	assert.NoError(t, err)
	assert.Len(t, report.ValidatingWebhooks, 1)
	assert.False(t, report.ValidatingWebhooks[0].Healthy)
	assert.NotEmpty(t, report.ValidatingWebhooks[0].Error)
	assert.True(t, report.HasIssues())
}

func TestDetectWithHealthCheck_NoEndpoints(t *testing.T) {
	vwc := &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "no-endpoints"},
		Webhooks: []admissionv1.ValidatingWebhook{
			{
				Name: "validate.example.com",
				Rules: []admissionv1.RuleWithOperations{
					{
						Rule: admissionv1.Rule{
							APIGroups:   []string{"apps"},
							APIVersions: []string{"v1"},
							Resources:   []string{"deployments"},
						},
					},
				},
				ClientConfig: admissionv1.WebhookClientConfig{
					Service: &admissionv1.ServiceReference{
						Namespace: "default",
						Name:      "empty-svc",
					},
				},
			},
		},
	}

	// Service exists but endpoints have no addresses
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "empty-svc",
			Namespace: "default",
		},
	}
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "empty-svc",
			Namespace: "default",
		},
		Subsets: []corev1.EndpointSubset{}, // No subsets
	}

	clientset := fake.NewSimpleClientset(vwc, svc, endpoints)
	client := &kube.Client{Clientset: clientset}
	detector := NewWebhookDetector(client)

	ctx := context.Background()

	report, err := detector.DetectWithHealthCheck(ctx, schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	})
	assert.NoError(t, err)
	assert.Len(t, report.ValidatingWebhooks, 1)
	assert.False(t, report.ValidatingWebhooks[0].Healthy)
	assert.Equal(t, "no ready endpoints available", report.ValidatingWebhooks[0].Error)
}

func TestCountReadyAddresses(t *testing.T) {
	tests := []struct {
		name      string
		endpoints *corev1.Endpoints
		expected  int
	}{
		{
			name:      "nil subsets",
			endpoints: &corev1.Endpoints{},
			expected:  0,
		},
		{
			name: "one subset with addresses",
			endpoints: &corev1.Endpoints{
				Subsets: []corev1.EndpointSubset{
					{
						Addresses: []corev1.EndpointAddress{
							{IP: "10.0.0.1"},
							{IP: "10.0.0.2"},
						},
					},
				},
			},
			expected: 2,
		},
		{
			name: "multiple subsets",
			endpoints: &corev1.Endpoints{
				Subsets: []corev1.EndpointSubset{
					{Addresses: []corev1.EndpointAddress{{IP: "10.0.0.1"}}},
					{Addresses: []corev1.EndpointAddress{{IP: "10.0.0.2"}, {IP: "10.0.0.3"}}},
				},
			},
			expected: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			count := countReadyAddresses(tc.endpoints)
			assert.Equal(t, tc.expected, count)
		})
	}
}

func TestGenerateWebhookGuidance(t *testing.T) {
	tests := []struct {
		name           string
		webhook        types.WebhookInfo
		expectContains []string
	}{
		{
			name: "validating webhook with service",
			webhook: types.WebhookInfo{
				Name:          "my-webhook",
				Type:          "validating",
				Error:         "no ready endpoints available",
				ServiceRef:    "kube-system/webhook-svc",
				FailurePolicy: "Fail",
			},
			expectContains: []string{
				"my-webhook",
				"no ready endpoints available",
				"kubectl delete validatingwebhookconfiguration",
				"kube-system",
				"webhook-svc",
				"FailurePolicy=Fail",
			},
		},
		{
			name: "mutating webhook",
			webhook: types.WebhookInfo{
				Name:          "mutate-webhook",
				Type:          "mutating",
				FailurePolicy: "Ignore",
			},
			expectContains: []string{
				"mutate-webhook",
				"kubectl delete mutatingwebhookconfiguration",
				"FailurePolicy=Ignore",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			guidance := GenerateWebhookGuidance(tc.webhook)
			for _, expected := range tc.expectContains {
				assert.Contains(t, guidance, expected)
			}
		})
	}
}

func TestWebhookErrorTypeDescription(t *testing.T) {
	tests := []struct {
		errorType  string
		expectDesc string
	}{
		{"webhook_denied", "The webhook actively denied the request"},
		{"webhook_failed", "The webhook call failed"},
		{"webhook_timeout", "The webhook call timed out"},
		{"webhook_unavailable", "The webhook service is unavailable (connection refused)"},
		{"webhook_no_endpoints", "The webhook service has no available endpoints"},
		{"webhook_service_missing", "The webhook service does not exist"},
		{"webhook_internal_error", "An internal error occurred calling the webhook"},
		{"unknown", "Unknown webhook error"},
	}

	for _, tc := range tests {
		t.Run(tc.errorType, func(t *testing.T) {
			desc := WebhookErrorTypeDescription(tc.errorType)
			assert.Equal(t, tc.expectDesc, desc)
		})
	}
}

func TestMatchesGVR(t *testing.T) {
	rules := []admissionv1.RuleWithOperations{
		{
			Rule: admissionv1.Rule{
				APIGroups:   []string{"apps"},
				APIVersions: []string{"v1"},
				Resources:   []string{"deployments"},
			},
		},
	}

	assert.True(t, matchesGVR(rules, schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}))
	assert.False(t, matchesGVR(rules, schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}))
}

// Helper function
func stringPtr(s string) *string {
	return &s
}
