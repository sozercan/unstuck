package planner

import (
	"testing"

	"github.com/sozercan/unstuck/pkg/types"
	"github.com/stretchr/testify/assert"
)

func TestCalculateRiskLevel_EmptyActions(t *testing.T) {
	result := CalculateRiskLevel(nil)
	assert.Equal(t, types.RiskNone, result)

	result = CalculateRiskLevel([]types.Action{})
	assert.Equal(t, types.RiskNone, result)
}

func TestCalculateRiskLevel_ReturnsHighest(t *testing.T) {
	tests := []struct {
		name     string
		actions  []types.Action
		expected types.RiskLevel
	}{
		{
			name: "all low",
			actions: []types.Action{
				{Risk: types.RiskLow},
				{Risk: types.RiskLow},
			},
			expected: types.RiskLow,
		},
		{
			name: "mixed low and medium",
			actions: []types.Action{
				{Risk: types.RiskLow},
				{Risk: types.RiskMedium},
			},
			expected: types.RiskMedium,
		},
		{
			name: "contains critical",
			actions: []types.Action{
				{Risk: types.RiskLow},
				{Risk: types.RiskCritical},
				{Risk: types.RiskHigh},
			},
			expected: types.RiskCritical,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateRiskLevel(tt.actions)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCompareRisk(t *testing.T) {
	tests := []struct {
		name     string
		a        types.RiskLevel
		b        types.RiskLevel
		expected int
	}{
		{"none equals none", types.RiskNone, types.RiskNone, 0},
		{"low > none", types.RiskLow, types.RiskNone, 1},
		{"low < medium", types.RiskLow, types.RiskMedium, -1},
		{"critical > high", types.RiskCritical, types.RiskHigh, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compareRisk(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRiskLevelFromEscalation(t *testing.T) {
	tests := []struct {
		escalation types.EscalationLevel
		expected   types.RiskLevel
	}{
		{types.EscalationInfo, types.RiskNone},
		{types.EscalationClean, types.RiskLow},
		{types.EscalationFinalizer, types.RiskMedium},
		{types.EscalationCRD, types.RiskHigh},
		{types.EscalationForce, types.RiskCritical},
		{types.EscalationLevel(99), types.RiskNone}, // unknown
	}

	for _, tt := range tests {
		t.Run(tt.escalation.String(), func(t *testing.T) {
			result := RiskLevelFromEscalation(tt.escalation)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCountActionsByLevel(t *testing.T) {
	actions := []types.Action{
		{EscalationLevel: types.EscalationInfo},
		{EscalationLevel: types.EscalationInfo},
		{EscalationLevel: types.EscalationClean},
		{EscalationLevel: types.EscalationFinalizer},
		{EscalationLevel: types.EscalationForce},
	}

	result := CountActionsByLevel(actions)

	assert.Equal(t, 2, result[types.EscalationInfo])
	assert.Equal(t, 1, result[types.EscalationClean])
	assert.Equal(t, 1, result[types.EscalationFinalizer])
	assert.Equal(t, 0, result[types.EscalationCRD])
	assert.Equal(t, 1, result[types.EscalationForce])
}

func TestFilterActionsByMaxLevel(t *testing.T) {
	actions := []types.Action{
		{ID: "l0", EscalationLevel: types.EscalationInfo},
		{ID: "l1", EscalationLevel: types.EscalationClean},
		{ID: "l2", EscalationLevel: types.EscalationFinalizer},
		{ID: "l3", EscalationLevel: types.EscalationCRD},
		{ID: "l4", EscalationLevel: types.EscalationForce},
	}

	// Filter up to L2
	result := FilterActionsByMaxLevel(actions, types.EscalationFinalizer)

	assert.Len(t, result, 3)
	assert.Equal(t, "l0", result[0].ID)
	assert.Equal(t, "l1", result[1].ID)
	assert.Equal(t, "l2", result[2].ID)
}

func TestHasForceActions(t *testing.T) {
	tests := []struct {
		name     string
		actions  []types.Action
		expected bool
	}{
		{
			name:     "empty",
			actions:  nil,
			expected: false,
		},
		{
			name: "no force actions",
			actions: []types.Action{
				{EscalationLevel: types.EscalationInfo, RequiresForce: false},
				{EscalationLevel: types.EscalationFinalizer, RequiresForce: false},
			},
			expected: false,
		},
		{
			name: "has force action",
			actions: []types.Action{
				{EscalationLevel: types.EscalationInfo, RequiresForce: false},
				{EscalationLevel: types.EscalationForce, RequiresForce: true},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasForceActions(tt.actions)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSummaryByRisk(t *testing.T) {
	actions := []types.Action{
		{Risk: types.RiskLow},
		{Risk: types.RiskLow},
		{Risk: types.RiskMedium},
		{Risk: types.RiskHigh},
		{Risk: types.RiskCritical},
	}

	result := SummaryByRisk(actions)

	assert.Equal(t, 2, result[types.RiskLow])
	assert.Equal(t, 1, result[types.RiskMedium])
	assert.Equal(t, 1, result[types.RiskHigh])
	assert.Equal(t, 1, result[types.RiskCritical])
}

func TestGenerateCommands(t *testing.T) {
	actions := []types.Action{
		{Command: "kubectl get pods"},
		{Command: "kubectl delete pod bar"},
		{Command: ""},
	}

	result := GenerateCommands(actions)

	assert.Len(t, result, 2)
	assert.Contains(t, result, "kubectl get pods")
	assert.Contains(t, result, "kubectl delete pod bar")
}
