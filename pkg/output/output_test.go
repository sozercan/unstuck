package output

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sozercan/unstuck/pkg/types"
)

func TestPrinter_PrintDiagnosis_JSON(t *testing.T) {
	report := &types.DiagnosisReport{
		Target: types.ResourceRef{
			Kind:       "Namespace",
			APIVersion: "v1",
			Name:       "test-ns",
		},
		TargetType:  types.TargetTypeNamespace,
		Status:      "Active",
		DiagnosedAt: time.Now(),
	}

	var buf bytes.Buffer
	printer := NewPrinter("json", false)
	printer.SetOutput(&buf)

	err := printer.PrintDiagnosis(report)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)

	assert.Equal(t, "Active", result["status"])
	assert.Equal(t, "namespace", result["targetType"])
}

func TestPrinter_PrintDiagnosis_Text_Healthy(t *testing.T) {
	report := &types.DiagnosisReport{
		Target: types.ResourceRef{
			Kind:       "Namespace",
			APIVersion: "v1",
			Name:       "default",
		},
		TargetType:  types.TargetTypeNamespace,
		Status:      "Active",
		DiagnosedAt: time.Now(),
	}

	var buf bytes.Buffer
	printer := NewPrinter("text", false)
	printer.SetOutput(&buf)

	err := printer.PrintDiagnosis(report)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "DIAGNOSIS")
	assert.Contains(t, output, "Namespace")
	assert.Contains(t, output, "default")
	assert.Contains(t, output, "Active")
	assert.Contains(t, output, "not in Terminating state")
}

func TestPrinter_PrintDiagnosis_Text_Terminating(t *testing.T) {
	now := time.Now()
	report := &types.DiagnosisReport{
		Target: types.ResourceRef{
			Kind:       "Namespace",
			APIVersion: "v1",
			Name:       "stuck-ns",
		},
		TargetType:        types.TargetTypeNamespace,
		Status:            "Terminating",
		DeletionTimestamp: &now,
		TerminatingFor:    "2h",
		RootCause:         "CR instances stuck with unsatisfied finalizers",
		Blockers: []types.Blocker{
			{
				ResourceRef: types.ResourceRef{
					Kind:      "Certificate",
					Namespace: "stuck-ns",
					Name:      "my-cert",
				},
				Finalizers: []string{"cert-manager.io/finalizer"},
			},
		},
		Recommendations: []string{"Use unstuck plan to remediate"},
		DiagnosedAt:     time.Now(),
	}

	var buf bytes.Buffer
	printer := NewPrinter("text", false)
	printer.SetOutput(&buf)

	err := printer.PrintDiagnosis(report)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "DIAGNOSIS")
	assert.Contains(t, output, "Terminating")
	assert.Contains(t, output, "BLOCKERS")
	assert.Contains(t, output, "Certificate")
	assert.Contains(t, output, "RECOMMENDATION")
}

func TestJSONPrinter_PrintDiagnosis(t *testing.T) {
	report := &types.DiagnosisReport{
		Target: types.ResourceRef{
			Kind: "Namespace",
			Name: "test",
		},
		Status:      "Active",
		DiagnosedAt: time.Now(),
	}

	var buf bytes.Buffer
	printer := NewJSONPrinter(true)
	printer.SetOutput(&buf)

	err := printer.PrintDiagnosis(report)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "Active", result["status"])
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"this is a long string", 10, "this is..."},
		{"exactly10!", 10, "exactly10!"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}
