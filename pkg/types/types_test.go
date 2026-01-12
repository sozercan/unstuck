package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestResourceRef_String(t *testing.T) {
	tests := []struct {
		name     string
		ref      ResourceRef
		expected string
	}{
		{
			name: "with namespace",
			ref: ResourceRef{
				Kind:      "Certificate",
				Namespace: "cert-manager",
				Name:      "my-cert",
			},
			expected: "Certificate/cert-manager/my-cert",
		},
		{
			name: "without namespace",
			ref: ResourceRef{
				Kind: "Namespace",
				Name: "default",
			},
			expected: "Namespace/default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.ref.String())
		})
	}
}

func TestBlocker_IsTerminating(t *testing.T) {
	now := metav1.Now()

	tests := []struct {
		name     string
		blocker  Blocker
		expected bool
	}{
		{
			name: "with deletion timestamp",
			blocker: Blocker{
				DeletionTimestamp: &now,
			},
			expected: true,
		},
		{
			name:     "without deletion timestamp",
			blocker:  Blocker{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.blocker.IsTerminating())
		})
	}
}

func TestDiagnosisReport_IsHealthy(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		expected bool
	}{
		{"active status", "Active", true},
		{"bound status", "Bound", true},
		{"empty status", "", true},
		{"terminating status", "Terminating", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := &DiagnosisReport{Status: tt.status}
			assert.Equal(t, tt.expected, report.IsHealthy())
		})
	}
}

func TestDiagnosisReport_IsTerminating(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		expected bool
	}{
		{"terminating status", "Terminating", true},
		{"active status", "Active", false},
		{"empty status", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := &DiagnosisReport{Status: tt.status}
			assert.Equal(t, tt.expected, report.IsTerminating())
		})
	}
}

func TestDiagnosisReport_HasBlockers(t *testing.T) {
	tests := []struct {
		name     string
		blockers []Blocker
		expected bool
	}{
		{
			name:     "with blockers",
			blockers: []Blocker{{ResourceRef: ResourceRef{Name: "test"}}},
			expected: true,
		},
		{
			name:     "without blockers",
			blockers: nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := &DiagnosisReport{Blockers: tt.blockers}
			assert.Equal(t, tt.expected, report.HasBlockers())
		})
	}
}

func TestEscalationLevel_String(t *testing.T) {
	tests := []struct {
		level    EscalationLevel
		expected string
	}{
		{EscalationInfo, "L0 (Informational)"},
		{EscalationClean, "L1 (Clean Deletion)"},
		{EscalationFinalizer, "L2 (Finalizer Removal)"},
		{EscalationCRD, "L3 (CRD Finalizer)"},
		{EscalationForce, "L4 (Force Finalize)"},
		{EscalationLevel(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.level.String())
		})
	}
}

func TestEscalationLevel_RequiresForce(t *testing.T) {
	tests := []struct {
		level    EscalationLevel
		expected bool
	}{
		{EscalationInfo, false},
		{EscalationClean, false},
		{EscalationFinalizer, false},
		{EscalationCRD, true},
		{EscalationForce, true},
	}

	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.level.RequiresForce())
		})
	}
}

func TestDiagnosisReport_TotalBlockerCount(t *testing.T) {
	report := &DiagnosisReport{
		Blockers: []Blocker{
			{ResourceRef: ResourceRef{Name: "a"}},
			{ResourceRef: ResourceRef{Name: "b"}},
			{ResourceRef: ResourceRef{Name: "c"}},
		},
	}
	assert.Equal(t, 3, report.TotalBlockerCount())
}

func TestDiagnosisReport_DiagnosedAt(t *testing.T) {
	now := time.Now()
	report := &DiagnosisReport{
		DiagnosedAt: now,
	}
	assert.Equal(t, now, report.DiagnosedAt)
}
