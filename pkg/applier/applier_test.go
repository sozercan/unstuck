package applier

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/sozercan/unstuck/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewApplier(t *testing.T) {
	opts := Options{
		DryRun:          true,
		ContinueOnError: true,
		AutoConfirm:     true,
		Verbose:         true,
	}

	app := NewApplier(nil, opts)

	assert.True(t, app.dryRun)
	assert.True(t, app.continueOnError)
	assert.True(t, app.autoConfirm)
	assert.True(t, app.verbose)
	assert.NotNil(t, app.output)
}

func TestApplier_Apply_NilPlan(t *testing.T) {
	app := NewApplier(nil, Options{})

	result, err := app.Apply(context.Background(), nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "cannot be nil")
}

func TestApplier_Apply_EmptyPlan(t *testing.T) {
	app := NewApplier(nil, Options{DryRun: true})

	plan := &types.Plan{
		Target:  types.ResourceRef{Kind: "Namespace", Name: "test"},
		Actions: []types.Action{},
	}

	result, err := app.Apply(context.Background(), plan)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.TotalActions)
	assert.Equal(t, 0, result.Succeeded)
	assert.Equal(t, 0, result.Failed)
	assert.Equal(t, 0, result.ExitCode)
}

func TestApplier_Apply_DryRun(t *testing.T) {
	buf := &bytes.Buffer{}
	app := NewApplier(nil, Options{
		DryRun: true,
		Output: buf,
	})

	plan := &types.Plan{
		Target: types.ResourceRef{Kind: "Namespace", Name: "test"},
		Actions: []types.Action{
			{
				ID:              "action-1",
				Type:            types.ActionInspect,
				Description:     "Inspect namespace",
				EscalationLevel: types.EscalationInfo,
				Target:          types.ResourceRef{Kind: "Namespace", Name: "test"},
				Command:         "kubectl get namespace test",
				Risk:            types.RiskNone,
			},
			{
				ID:              "action-2",
				Type:            types.ActionPatch,
				Description:     "Remove finalizer",
				EscalationLevel: types.EscalationFinalizer,
				Target:          types.ResourceRef{Kind: "Pod", Name: "my-pod", Namespace: "test"},
				Command:         "kubectl patch pod my-pod -n test -p '{}'",
				Risk:            types.RiskMedium,
			},
		},
	}

	result, err := app.Apply(context.Background(), plan)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 2, result.TotalActions)
	assert.Equal(t, 2, result.Succeeded)
	assert.Equal(t, 0, result.Failed)
	assert.Equal(t, 0, result.Skipped)
	assert.Equal(t, 0, result.ExitCode)

	// Verify output contains dry-run info
	output := buf.String()
	assert.Contains(t, output, "DRY-RUN")
	assert.Contains(t, output, "Inspect namespace")
}

func TestApplier_CalculateExitCode(t *testing.T) {
	app := NewApplier(nil, Options{})

	tests := []struct {
		name     string
		result   *types.ApplyResult
		wantCode int
	}{
		{
			name: "all succeeded",
			result: &types.ApplyResult{
				TotalActions: 3,
				Succeeded:    3,
				Failed:       0,
				Skipped:      0,
			},
			wantCode: 0,
		},
		{
			name: "all failed",
			result: &types.ApplyResult{
				TotalActions: 3,
				Succeeded:    0,
				Failed:       3,
				Skipped:      0,
			},
			wantCode: 1,
		},
		{
			name: "all skipped",
			result: &types.ApplyResult{
				TotalActions: 3,
				Succeeded:    0,
				Failed:       0,
				Skipped:      3,
			},
			wantCode: 1,
		},
		{
			name: "partial success",
			result: &types.ApplyResult{
				TotalActions: 3,
				Succeeded:    1,
				Failed:       2,
				Skipped:      0,
			},
			wantCode: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := app.calculateExitCode(tt.result)
			assert.Equal(t, tt.wantCode, code)
		})
	}
}

func TestApplier_FormatTarget(t *testing.T) {
	app := NewApplier(nil, Options{})

	tests := []struct {
		name     string
		target   types.ResourceRef
		expected string
	}{
		{
			name: "with namespace",
			target: types.ResourceRef{
				Kind:      "Pod",
				Namespace: "default",
				Name:      "my-pod",
			},
			expected: "Pod/default/my-pod\n",
		},
		{
			name: "without namespace",
			target: types.ResourceRef{
				Kind: "Namespace",
				Name: "test-ns",
			},
			expected: "Namespace/test-ns\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := app.formatTarget(tt.target)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestApplyResult_Duration(t *testing.T) {
	start := time.Now()
	time.Sleep(10 * time.Millisecond)
	end := time.Now()

	result := &types.ApplyResult{
		StartTime: start,
		EndTime:   end,
	}

	duration := result.EndTime.Sub(result.StartTime)
	assert.True(t, duration >= 10*time.Millisecond)
}
