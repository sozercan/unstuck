package types

import (
	"time"
)

// ActionType represents the type of remediation action
type ActionType string

const (
	ActionInspect  ActionType = "inspect"  // Read-only check
	ActionList     ActionType = "list"     // Enumerate resources
	ActionPatch    ActionType = "patch"    // Modify object
	ActionDelete   ActionType = "delete"   // Delete object
	ActionFinalize ActionType = "finalize" // Force finalize
	ActionWait     ActionType = "wait"     // Wait for condition
)

// RiskLevel represents the risk level of an action
type RiskLevel string

const (
	RiskNone     RiskLevel = "none"
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// EscalationLevel represents the escalation level of an action
type EscalationLevel int

const (
	// EscalationInfo is read-only, no changes
	EscalationInfo EscalationLevel = 0
	// EscalationClean attempts clean deletion with controller
	EscalationClean EscalationLevel = 1
	// EscalationFinalizer removes finalizers from resources
	EscalationFinalizer EscalationLevel = 2
	// EscalationCRD removes CRD cleanup finalizer
	EscalationCRD EscalationLevel = 3
	// EscalationForce force-finalizes namespace
	EscalationForce EscalationLevel = 4
)

// String returns the string representation of the escalation level
func (e EscalationLevel) String() string {
	switch e {
	case EscalationInfo:
		return "L0 (Informational)"
	case EscalationClean:
		return "L1 (Clean Deletion)"
	case EscalationFinalizer:
		return "L2 (Finalizer Removal)"
	case EscalationCRD:
		return "L3 (CRD Finalizer)"
	case EscalationForce:
		return "L4 (Force Finalize)"
	default:
		return "Unknown"
	}
}

// RequiresForce returns true if this escalation level requires --allow-force
func (e EscalationLevel) RequiresForce() bool {
	return e >= EscalationCRD
}

// Action represents a single remediation action
type Action struct {
	ID              string          `json:"id"`
	Type            ActionType      `json:"type"`
	EscalationLevel EscalationLevel `json:"escalationLevel"`
	Description     string          `json:"description"`
	Target          ResourceRef     `json:"target"`
	Operation       string          `json:"operation"`
	Command         string          `json:"command"` // kubectl equivalent
	Risk            RiskLevel       `json:"risk"`
	RequiresForce   bool            `json:"requiresForce"`
	ExpectedResult  string          `json:"expectedResult"`
	DependsOn       []string        `json:"dependsOn,omitempty"`
	Timeout         time.Duration   `json:"timeout,omitempty"`
}

// Plan represents a remediation plan
type Plan struct {
	Target        ResourceRef `json:"target"`
	RiskLevel     RiskLevel   `json:"riskLevel"`
	MaxEscalation EscalationLevel `json:"maxEscalation"`
	Actions       []Action    `json:"actions"`
	Commands      []string    `json:"commands,omitempty"` // kubectl commands for dry-run
}

// ActionResult represents the result of executing an action
type ActionResult struct {
	Action     Action      `json:"action"`
	Success    bool        `json:"success"`
	Error      string      `json:"error,omitempty"`
	Before     interface{} `json:"before,omitempty"`
	After      interface{} `json:"after,omitempty"`
	ExecutedAt time.Time   `json:"executedAt"`
	Duration   string      `json:"duration"`
}

// ApplyResult represents the result of applying a plan
type ApplyResult struct {
	StartTime    time.Time      `json:"startTime"`
	EndTime      time.Time      `json:"endTime"`
	TotalActions int            `json:"totalActions"`
	Succeeded    int            `json:"succeeded"`
	Failed       int            `json:"failed"`
	Skipped      int            `json:"skipped"`
	Actions      []ActionResult `json:"actions"`
	ExitCode     int            `json:"exitCode"` // 0=success, 1=failure, 2=partial
}
