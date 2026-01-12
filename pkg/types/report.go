package types

import (
	"time"
)

// TargetType represents the type of resource being diagnosed
type TargetType string

const (
	TargetTypeNamespace TargetType = "namespace"
	TargetTypeCRD       TargetType = "crd"
	TargetTypeResource  TargetType = "resource"
)

// DiagnosisReport contains the full diagnosis of a stuck resource
type DiagnosisReport struct {
	// Target information
	Target     ResourceRef `json:"target"`
	TargetType TargetType  `json:"targetType"`

	// Status
	Status            string     `json:"status"` // Active, Terminating
	DeletionTimestamp *time.Time `json:"deletionTimestamp,omitempty"`
	TerminatingFor    string     `json:"terminatingFor,omitempty"` // Human-readable duration

	// Root cause analysis
	RootCause   string   `json:"rootCause,omitempty"`
	Finalizers  []string `json:"finalizers,omitempty"`

	// Blockers
	Blockers         []Blocker          `json:"blockers,omitempty"`
	DiscoveryFailures []DiscoveryFailure `json:"discoveryFailures,omitempty"`
	WebhookIssues    []WebhookInfo      `json:"webhookIssues,omitempty"`

	// Controller status (for namespace diagnosis)
	Controllers []ControllerStatus `json:"controllers,omitempty"`

	// Namespace conditions (parsed from status)
	Conditions []ConditionSummary `json:"conditions,omitempty"`

	// For CRD diagnosis: instances remaining
	InstanceCount     int         `json:"instanceCount,omitempty"`
	InstancesByNS     map[string]int `json:"instancesByNamespace,omitempty"`

	// Recommendations
	Recommendations []string `json:"recommendations,omitempty"`

	// Metadata
	DiagnosedAt time.Time `json:"diagnosedAt"`
}

// ConditionSummary summarizes a namespace/resource condition
type ConditionSummary struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// IsHealthy returns true if the target is not stuck
func (r *DiagnosisReport) IsHealthy() bool {
	return r.Status == "Active" || r.Status == "Bound" || r.Status == ""
}

// IsTerminating returns true if the target is in terminating state
func (r *DiagnosisReport) IsTerminating() bool {
	return r.Status == "Terminating"
}

// HasBlockers returns true if there are blocking resources
func (r *DiagnosisReport) HasBlockers() bool {
	return len(r.Blockers) > 0
}

// HasDiscoveryFailures returns true if there are discovery failures
func (r *DiagnosisReport) HasDiscoveryFailures() bool {
	return len(r.DiscoveryFailures) > 0
}

// HasWebhookIssues returns true if there are webhook issues
func (r *DiagnosisReport) HasWebhookIssues() bool {
	return len(r.WebhookIssues) > 0
}

// TotalBlockerCount returns the total number of blockers
func (r *DiagnosisReport) TotalBlockerCount() int {
	return len(r.Blockers)
}
