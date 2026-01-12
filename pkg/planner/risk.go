package planner

import (
	"github.com/sozercan/unstuck/pkg/types"
)

// CalculateRiskLevel determines the overall risk level of a plan
func CalculateRiskLevel(actions []types.Action) types.RiskLevel {
	if len(actions) == 0 {
		return types.RiskNone
	}

	maxRisk := types.RiskNone
	for _, action := range actions {
		if compareRisk(action.Risk, maxRisk) > 0 {
			maxRisk = action.Risk
		}
	}

	return maxRisk
}

// compareRisk compares two risk levels
// Returns: -1 if a < b, 0 if a == b, 1 if a > b
func compareRisk(a, b types.RiskLevel) int {
	ranks := map[types.RiskLevel]int{
		types.RiskNone:     0,
		types.RiskLow:      1,
		types.RiskMedium:   2,
		types.RiskHigh:     3,
		types.RiskCritical: 4,
	}

	ra, ok := ranks[a]
	if !ok {
		ra = 0
	}
	rb, ok := ranks[b]
	if !ok {
		rb = 0
	}

	switch {
	case ra < rb:
		return -1
	case ra > rb:
		return 1
	default:
		return 0
	}
}

// GenerateCommands extracts kubectl commands from actions
func GenerateCommands(actions []types.Action) []string {
	var commands []string
	for _, action := range actions {
		if action.Command != "" {
			commands = append(commands, action.Command)
		}
	}
	return commands
}

// RiskLevelFromEscalation returns the typical risk level for an escalation level
func RiskLevelFromEscalation(level types.EscalationLevel) types.RiskLevel {
	switch level {
	case types.EscalationInfo:
		return types.RiskNone
	case types.EscalationClean:
		return types.RiskLow
	case types.EscalationFinalizer:
		return types.RiskMedium
	case types.EscalationCRD:
		return types.RiskHigh
	case types.EscalationForce:
		return types.RiskCritical
	default:
		return types.RiskNone
	}
}

// CountActionsByLevel counts actions by their escalation level
func CountActionsByLevel(actions []types.Action) map[types.EscalationLevel]int {
	counts := make(map[types.EscalationLevel]int)
	for _, action := range actions {
		counts[action.EscalationLevel]++
	}
	return counts
}

// FilterActionsByMaxLevel filters actions to only include those at or below the max level
func FilterActionsByMaxLevel(actions []types.Action, maxLevel types.EscalationLevel) []types.Action {
	var filtered []types.Action
	for _, action := range actions {
		if action.EscalationLevel <= maxLevel {
			filtered = append(filtered, action)
		}
	}
	return filtered
}

// HasForceActions returns true if any actions require --allow-force
func HasForceActions(actions []types.Action) bool {
	for _, action := range actions {
		if action.RequiresForce {
			return true
		}
	}
	return false
}

// SummaryByRisk returns a summary of actions grouped by risk level
func SummaryByRisk(actions []types.Action) map[types.RiskLevel]int {
	summary := make(map[types.RiskLevel]int)
	for _, action := range actions {
		summary[action.Risk]++
	}
	return summary
}
