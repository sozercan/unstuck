package planner

import (
	"testing"

	"github.com/sozercan/unstuck/pkg/types"
	"github.com/stretchr/testify/assert"
)

func TestOrderByDependencies_Empty(t *testing.T) {
	result := OrderByDependencies(nil)
	assert.Empty(t, result)

	result = OrderByDependencies([]types.Action{})
	assert.Empty(t, result)
}

func TestOrderByDependencies_SortsByEscalation(t *testing.T) {
	actions := []types.Action{
		{ID: "l2", EscalationLevel: types.EscalationFinalizer, Target: types.ResourceRef{Kind: "A", Name: "a"}},
		{ID: "l0", EscalationLevel: types.EscalationInfo, Target: types.ResourceRef{Kind: "B", Name: "b"}},
		{ID: "l1", EscalationLevel: types.EscalationClean, Target: types.ResourceRef{Kind: "C", Name: "c"}},
	}

	result := OrderByDependencies(actions)

	assert.Len(t, result, 3)
	assert.Equal(t, types.EscalationInfo, result[0].EscalationLevel)
	assert.Equal(t, types.EscalationClean, result[1].EscalationLevel)
	assert.Equal(t, types.EscalationFinalizer, result[2].EscalationLevel)
}

func TestOrderByDependencies_SortsByResourcePriority(t *testing.T) {
	// All same escalation level, should sort by resource type
	actions := []types.Action{
		{ID: "ns", EscalationLevel: types.EscalationFinalizer, Target: types.ResourceRef{Kind: "Namespace", Name: "a"}},
		{ID: "pod", EscalationLevel: types.EscalationFinalizer, Target: types.ResourceRef{Kind: "Pod", Name: "b"}},
		{ID: "crd", EscalationLevel: types.EscalationFinalizer, Target: types.ResourceRef{Kind: "CustomResourceDefinition", Name: "c"}},
		{ID: "cert", EscalationLevel: types.EscalationFinalizer, Target: types.ResourceRef{Kind: "Certificate", Name: "d"}},
	}

	result := OrderByDependencies(actions)

	assert.Len(t, result, 4)
	// Pod (10) < Certificate (30) < CRD (100) < Namespace (200)
	assert.Equal(t, "Pod", result[0].Target.Kind)
	assert.Equal(t, "Certificate", result[1].Target.Kind)
	assert.Equal(t, "CustomResourceDefinition", result[2].Target.Kind)
	assert.Equal(t, "Namespace", result[3].Target.Kind)
}

func TestOrderByDependencies_SortsByNameWithinSameType(t *testing.T) {
	actions := []types.Action{
		{ID: "c", EscalationLevel: types.EscalationFinalizer, Target: types.ResourceRef{Kind: "Pod", Name: "charlie"}},
		{ID: "a", EscalationLevel: types.EscalationFinalizer, Target: types.ResourceRef{Kind: "Pod", Name: "alpha"}},
		{ID: "b", EscalationLevel: types.EscalationFinalizer, Target: types.ResourceRef{Kind: "Pod", Name: "bravo"}},
	}

	result := OrderByDependencies(actions)

	assert.Len(t, result, 3)
	assert.Equal(t, "alpha", result[0].Target.Name)
	assert.Equal(t, "bravo", result[1].Target.Name)
	assert.Equal(t, "charlie", result[2].Target.Name)
}

func TestResourcePriority(t *testing.T) {
	tests := []struct {
		kind     string
		expected int
	}{
		{"Pod", 10},
		{"ConfigMap", 10},
		{"Secret", 10},
		{"Service", 10},
		{"Deployment", 20},
		{"StatefulSet", 20},
		{"DaemonSet", 20},
		{"Certificate", 30},
		{"Issuer", 30},
		{"CustomResourceDefinition", 100},
		{"Namespace", 200},
		{"UnknownResource", 50},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			result := resourcePriority(tt.kind)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResourceKey(t *testing.T) {
	tests := []struct {
		name     string
		ref      types.ResourceRef
		expected string
	}{
		{
			name:     "with namespace",
			ref:      types.ResourceRef{Kind: "Pod", Namespace: "default", Name: "my-pod"},
			expected: "Pod/default/my-pod",
		},
		{
			name:     "without namespace",
			ref:      types.ResourceRef{Kind: "Namespace", Name: "kube-system"},
			expected: "Namespace/kube-system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resourceKey(tt.ref)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTopologicalSort_NoOwnerRefs(t *testing.T) {
	actions := []types.Action{
		{ID: "a", Target: types.ResourceRef{Kind: "Pod", Name: "a"}},
		{ID: "b", Target: types.ResourceRef{Kind: "Pod", Name: "b"}},
	}

	// With empty owner refs, should fall back to OrderByDependencies
	result := TopologicalSort(actions, nil)

	assert.Len(t, result, 2)
}

func TestTopologicalSort_Empty(t *testing.T) {
	result := TopologicalSort(nil, nil)
	assert.Empty(t, result)
}
