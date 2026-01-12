package planner

import (
	"testing"

	"github.com/sozercan/unstuck/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPlanner(t *testing.T) {
	opts := Options{
		MaxEscalation: types.EscalationFinalizer,
		AllowForce:    false,
	}

	p := NewPlanner(opts)

	assert.Equal(t, types.EscalationFinalizer, p.maxEscalation)
	assert.False(t, p.allowForce)
}

func TestPlanner_Plan_NilDiagnosis(t *testing.T) {
	p := NewPlanner(Options{MaxEscalation: types.EscalationFinalizer})

	plan, err := p.Plan(nil)

	assert.Error(t, err)
	assert.Nil(t, plan)
}

func TestPlanner_Plan_HealthyNamespace(t *testing.T) {
	p := NewPlanner(Options{MaxEscalation: types.EscalationFinalizer})

	diagnosis := &types.DiagnosisReport{
		Target: types.ResourceRef{
			Kind: "Namespace",
			Name: "default",
		},
		TargetType: types.TargetTypeNamespace,
		Status:     "Active",
	}

	plan, err := p.Plan(diagnosis)

	require.NoError(t, err)
	assert.NotNil(t, plan)
	assert.Equal(t, "default", plan.Target.Name)
	assert.Empty(t, plan.Actions)
	assert.Empty(t, plan.Commands)
}

func TestPlanner_Plan_TerminatingWithBlockers(t *testing.T) {
	p := NewPlanner(Options{MaxEscalation: types.EscalationFinalizer})

	diagnosis := &types.DiagnosisReport{
		Target: types.ResourceRef{
			Kind: "Namespace",
			Name: "cert-manager",
		},
		TargetType: types.TargetTypeNamespace,
		Status:     "Terminating",
		Blockers: []types.Blocker{
			{
				ResourceRef: types.ResourceRef{
					Kind:       "Certificate",
					APIVersion: "cert-manager.io/v1",
					Namespace:  "cert-manager",
					Name:       "my-cert",
				},
				Finalizers: []string{"cert-manager.io/finalizer"},
			},
			{
				ResourceRef: types.ResourceRef{
					Kind:       "Certificate",
					APIVersion: "cert-manager.io/v1",
					Namespace:  "cert-manager",
					Name:       "other-cert",
				},
				Finalizers: []string{"cert-manager.io/finalizer"},
			},
		},
	}

	plan, err := p.Plan(diagnosis)

	require.NoError(t, err)
	assert.NotNil(t, plan)
	// Should have L0 inspect + list actions, plus L2 finalizer removal actions
	// 2 L0 actions + 2 L2 actions = 4 total
	assert.Len(t, plan.Actions, 4)

	// Verify action IDs are assigned
	for i, action := range plan.Actions {
		assert.NotEmpty(t, action.ID, "action %d should have ID", i)
	}

	// Verify commands are generated for L2 actions
	assert.Len(t, plan.Commands, 2)
	for _, cmd := range plan.Commands {
		assert.Contains(t, cmd, "kubectl patch")
	}

	// Risk level should be medium (L2)
	assert.Equal(t, types.RiskMedium, plan.RiskLevel)
}

func TestPlanner_Plan_L0Only(t *testing.T) {
	p := NewPlanner(Options{MaxEscalation: types.EscalationInfo})

	diagnosis := &types.DiagnosisReport{
		Target: types.ResourceRef{
			Kind: "Namespace",
			Name: "test-ns",
		},
		TargetType: types.TargetTypeNamespace,
		Status:     "Terminating",
		Blockers: []types.Blocker{
			{
				ResourceRef: types.ResourceRef{
					Kind: "Certificate",
					Name: "my-cert",
				},
				Finalizers: []string{"test-finalizer"},
			},
		},
	}

	plan, err := p.Plan(diagnosis)

	require.NoError(t, err)
	// Only L0 actions (inspect + list)
	assert.Len(t, plan.Actions, 2)
	for _, action := range plan.Actions {
		assert.Equal(t, types.EscalationInfo, action.EscalationLevel)
	}
	// No commands for L0 actions
	assert.Empty(t, plan.Commands)
}

func TestPlanner_Plan_CRDWithForce(t *testing.T) {
	p := NewPlanner(Options{
		MaxEscalation: types.EscalationCRD,
		AllowForce:    true,
	})

	diagnosis := &types.DiagnosisReport{
		Target: types.ResourceRef{
			Kind:       "CustomResourceDefinition",
			APIVersion: "apiextensions.k8s.io/v1",
			Name:       "certificates.cert-manager.io",
		},
		TargetType: types.TargetTypeCRD,
		Status:     "Terminating",
		Finalizers: []string{"customresourcecleanup.apiextensions.k8s.io"},
	}

	plan, err := p.Plan(diagnosis)

	require.NoError(t, err)
	// Should include L3 CRD finalizer action
	var hasL3 bool
	for _, action := range plan.Actions {
		if action.EscalationLevel == types.EscalationCRD {
			hasL3 = true
			assert.True(t, action.RequiresForce)
			assert.Equal(t, types.RiskHigh, action.Risk)
		}
	}
	assert.True(t, hasL3, "should have L3 action for CRD")
}

func TestPlanner_Plan_ForceFinalize(t *testing.T) {
	p := NewPlanner(Options{
		MaxEscalation: types.EscalationForce,
		AllowForce:    true,
	})

	diagnosis := &types.DiagnosisReport{
		Target: types.ResourceRef{
			Kind: "Namespace",
			Name: "broken-ns",
		},
		TargetType: types.TargetTypeNamespace,
		Status:     "Terminating",
		DiscoveryFailures: []types.DiscoveryFailure{
			{
				GroupVersion: "widgets.example.com/v1",
				Error:        "the server could not find the requested resource",
			},
		},
	}

	plan, err := p.Plan(diagnosis)

	require.NoError(t, err)
	// Should include L4 force-finalize action
	var hasL4 bool
	for _, action := range plan.Actions {
		if action.EscalationLevel == types.EscalationForce {
			hasL4 = true
			assert.True(t, action.RequiresForce)
			assert.Equal(t, types.RiskCritical, action.Risk)
			assert.Contains(t, action.Command, "finalize")
		}
	}
	assert.True(t, hasL4, "should have L4 action for discovery failure")
}

func TestPlanner_Plan_NoForceWithoutFlag(t *testing.T) {
	p := NewPlanner(Options{
		MaxEscalation: types.EscalationForce,
		AllowForce:    false, // Force not allowed
	})

	diagnosis := &types.DiagnosisReport{
		Target: types.ResourceRef{
			Kind: "Namespace",
			Name: "broken-ns",
		},
		TargetType: types.TargetTypeNamespace,
		Status:     "Terminating",
		DiscoveryFailures: []types.DiscoveryFailure{
			{
				GroupVersion: "widgets.example.com/v1",
				Error:        "the server could not find the requested resource",
			},
		},
	}

	plan, err := p.Plan(diagnosis)

	require.NoError(t, err)
	// Should NOT include L4 force-finalize action (force not allowed)
	for _, action := range plan.Actions {
		assert.Less(t, int(action.EscalationLevel), int(types.EscalationCRD),
			"should not have L3+ actions without allowForce")
	}
}

func TestGeneratePatchCommand(t *testing.T) {
	tests := []struct {
		name     string
		target   types.ResourceRef
		expected string
	}{
		{
			name: "with namespace",
			target: types.ResourceRef{
				Kind:      "Certificate",
				Namespace: "cert-manager",
				Name:      "my-cert",
			},
			expected: "kubectl patch Certificate my-cert -n cert-manager -p '{\"metadata\":{\"finalizers\":null}}' --type=merge",
		},
		{
			name: "without namespace",
			target: types.ResourceRef{
				Kind: "Namespace",
				Name: "test-ns",
			},
			expected: "kubectl patch Namespace test-ns -p '{\"metadata\":{\"finalizers\":null}}' --type=merge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generatePatchCommand(tt.target)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateCRDPatchCommand(t *testing.T) {
	result := generateCRDPatchCommand("certificates.cert-manager.io")

	assert.Contains(t, result, "kubectl patch crd certificates.cert-manager.io")
	assert.Contains(t, result, "finalizers")
}

func TestGenerateForceFinalize(t *testing.T) {
	result := generateForceFinalize("test-ns")

	assert.Contains(t, result, "kubectl get namespace test-ns")
	assert.Contains(t, result, "finalize")
	assert.Contains(t, result, "jq")
}

func TestHasAvailableController(t *testing.T) {
	tests := []struct {
		name        string
		controllers []types.ControllerStatus
		expected    bool
	}{
		{
			name:        "no controllers",
			controllers: nil,
			expected:    false,
		},
		{
			name: "available and ready",
			controllers: []types.ControllerStatus{
				{Name: "cert-manager", Available: true, Ready: true},
			},
			expected: true,
		},
		{
			name: "available but not ready",
			controllers: []types.ControllerStatus{
				{Name: "cert-manager", Available: true, Ready: false},
			},
			expected: false,
		},
		{
			name: "not available",
			controllers: []types.ControllerStatus{
				{Name: "cert-manager", Available: false, Ready: false},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagnosis := &types.DiagnosisReport{
				Controllers: tt.controllers,
			}
			result := hasAvailableController(diagnosis)
			assert.Equal(t, tt.expected, result)
		})
	}
}
