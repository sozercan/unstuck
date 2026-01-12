package applier

import (
	"testing"

	"github.com/sozercan/unstuck/pkg/types"
	"github.com/stretchr/testify/assert"
)

func TestNewExecutor(t *testing.T) {
	exec := NewExecutor(nil)
	assert.NotNil(t, exec)
	assert.Nil(t, exec.client)
}

func TestPluralize(t *testing.T) {
	tests := []struct {
		kind     string
		expected string
	}{
		{"Pod", "pods"},
		{"Service", "services"},
		{"Deployment", "deployments"},
		{"ConfigMap", "configmaps"},
		{"Secret", "secrets"},
		{"Namespace", "namespaces"},
		{"Endpoints", "endpoints"},
		{"Ingress", "ingresses"},
		{"NetworkPolicy", "networkpolicies"},
		{"PodSecurityPolicy", "podsecuritypolicies"},
		{"ResourceQuota", "resourcequotas"},
		{"LimitRange", "limitranges"},
		{"Certificate", "certificates"},
		{"Issuer", "issuers"},
		{"Status", "statuses"},
		{"Class", "classes"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			result := pluralize(tt.kind)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExecutor_ResolveGVR(t *testing.T) {
	exec := NewExecutor(nil)

	tests := []struct {
		name    string
		target  types.ResourceRef
		wantGrp string
		wantVer string
		wantRes string
	}{
		{
			name: "core resource",
			target: types.ResourceRef{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			wantGrp: "",
			wantVer: "v1",
			wantRes: "pods",
		},
		{
			name: "custom resource",
			target: types.ResourceRef{
				Kind:       "Certificate",
				APIVersion: "cert-manager.io/v1",
			},
			wantGrp: "cert-manager.io",
			wantVer: "v1",
			wantRes: "certificates",
		},
		{
			name: "apiextensions",
			target: types.ResourceRef{
				Kind:       "CustomResourceDefinition",
				APIVersion: "apiextensions.k8s.io/v1",
			},
			wantGrp: "apiextensions.k8s.io",
			wantVer: "v1",
			wantRes: "customresourcedefinitions",
		},
		{
			name: "empty apiversion defaults to v1",
			target: types.ResourceRef{
				Kind:       "Pod",
				APIVersion: "",
			},
			wantGrp: "",
			wantVer: "v1",
			wantRes: "pods",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gvr, err := exec.resolveGVR(tt.target)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantGrp, gvr.Group)
			assert.Equal(t, tt.wantVer, gvr.Version)
			assert.Equal(t, tt.wantRes, gvr.Resource)
		})
	}
}

func TestSnapshotJSON(t *testing.T) {
	tests := []struct {
		name     string
		snapshot interface{}
		expected string
	}{
		{
			name:     "nil snapshot",
			snapshot: nil,
			expected: "null",
		},
		{
			name: "simple map",
			snapshot: map[string]interface{}{
				"finalizers": []string{"test"},
				"phase":      "Active",
			},
			expected: `{"finalizers":["test"],"phase":"Active"}`,
		},
		{
			name:     "empty map",
			snapshot: map[string]interface{}{},
			expected: "{}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SnapshotJSON(tt.snapshot)
			assert.Equal(t, tt.expected, result)
		})
	}
}
