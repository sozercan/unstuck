package applier

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sozercan/unstuck/pkg/kube"
	"github.com/sozercan/unstuck/pkg/types"
)

// Options configures the applier behavior
type Options struct {
	// DryRun if true, only logs actions without executing
	DryRun bool
	// ContinueOnError if true, continues executing after errors
	ContinueOnError bool
	// AutoConfirm if true, skips confirmation prompts
	AutoConfirm bool
	// Verbose if true, enables detailed logging
	Verbose bool
	// Output for log messages (defaults to os.Stdout)
	Output io.Writer
}

// Applier executes remediation plans
type Applier struct {
	client          *kube.Client
	dryRun          bool
	continueOnError bool
	autoConfirm     bool
	verbose         bool
	output          io.Writer
	executor        *Executor
}

// NewApplier creates a new applier with the given options
func NewApplier(client *kube.Client, opts Options) *Applier {
	output := opts.Output
	if output == nil {
		output = os.Stdout
	}

	return &Applier{
		client:          client,
		dryRun:          opts.DryRun,
		continueOnError: opts.ContinueOnError,
		autoConfirm:     opts.AutoConfirm,
		verbose:         opts.Verbose,
		output:          output,
		executor:        NewExecutor(client),
	}
}

// Apply executes the given plan and returns the result
func (a *Applier) Apply(ctx context.Context, plan *types.Plan) (*types.ApplyResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("plan cannot be nil")
	}

	result := &types.ApplyResult{
		StartTime:    time.Now(),
		TotalActions: len(plan.Actions),
		Actions:      make([]types.ActionResult, 0, len(plan.Actions)),
	}

	for i, action := range plan.Actions {
		actionResult := types.ActionResult{
			Action:     action,
			ExecutedAt: time.Now(),
		}

		startTime := time.Now()

		// Check if action requires confirmation
		if action.RequiresForce && !a.dryRun && !a.autoConfirm {
			confirmed, err := a.confirm(action)
			if err != nil {
				actionResult.Error = fmt.Sprintf("confirmation error: %v", err)
				actionResult.Success = false
				result.Failed++
				result.Actions = append(result.Actions, actionResult)
				if !a.continueOnError {
					break
				}
				continue
			}
			if !confirmed {
				a.log("[%d/%d] SKIPPED: %s (user declined)\n", i+1, result.TotalActions, action.Description)
				result.Skipped++
				actionResult.Success = false
				actionResult.Error = "skipped by user"
				result.Actions = append(result.Actions, actionResult)
				continue
			}
		}

		// Capture before state
		var beforeState interface{}
		if a.verbose && !a.dryRun {
			beforeState, _ = a.executor.Snapshot(ctx, action.Target)
			actionResult.Before = beforeState
		}

		// Execute or simulate
		if a.dryRun {
			a.logDryRun(i+1, result.TotalActions, action)
			actionResult.Success = true
			result.Succeeded++
		} else {
			a.logAction(i+1, result.TotalActions, action)
			err := a.executor.Execute(ctx, action)
			if err != nil {
				actionResult.Error = err.Error()
				actionResult.Success = false
				result.Failed++
				a.log("  Result: FAILED (%v)\n", err)

				if !a.continueOnError {
					result.Actions = append(result.Actions, actionResult)
					break
				}
			} else {
				// Verify post-condition
				verified, verifyErr := a.executor.Verify(ctx, action)
				if verifyErr != nil {
					actionResult.Error = fmt.Sprintf("verification failed: %v", verifyErr)
					actionResult.Success = false
					result.Failed++
					a.log("  Result: VERIFICATION FAILED (%v)\n", verifyErr)
				} else if !verified {
					actionResult.Error = "post-condition not met"
					actionResult.Success = false
					result.Failed++
					a.log("  Result: POST-CONDITION NOT MET\n")
				} else {
					actionResult.Success = true
					result.Succeeded++
					a.log("  Result: SUCCESS (%s)\n", time.Since(startTime).Round(time.Millisecond))
				}

				// Capture after state
				if a.verbose {
					afterState, _ := a.executor.Snapshot(ctx, action.Target)
					actionResult.After = afterState
				}
			}
		}

		actionResult.Duration = time.Since(startTime).String()
		result.Actions = append(result.Actions, actionResult)
	}

	result.EndTime = time.Now()
	result.ExitCode = a.calculateExitCode(result)
	return result, nil
}

// confirm prompts the user to confirm a high-risk action
func (a *Applier) confirm(action types.Action) (bool, error) {
	fmt.Fprintf(a.output, "\n⚠️  HIGH-RISK ACTION (%s)\n", action.EscalationLevel)
	fmt.Fprintf(a.output, "  Target:  %s/%s\n", action.Target.Kind, action.Target.Name)
	fmt.Fprintf(a.output, "  Action:  %s\n", action.Description)
	fmt.Fprintf(a.output, "  Command: %s\n", action.Command)
	fmt.Fprintf(a.output, "  Risk:    %s\n\n", action.Risk)

	fmt.Fprint(a.output, "Proceed with this action? [y/N]: ")

	var response string
	_, err := fmt.Scanln(&response)
	if err != nil && err.Error() != "unexpected newline" {
		return false, nil // Treat as "no"
	}

	return response == "y" || response == "Y" || response == "yes" || response == "YES", nil
}

// logDryRun logs an action in dry-run mode
func (a *Applier) logDryRun(num, total int, action types.Action) {
	a.log("[%d/%d] DRY-RUN: %s\n", num, total, action.Description)
	a.log("  Target:  %s", a.formatTarget(action.Target))
	a.log("  Command: %s\n", action.Command)
	a.log("  Risk:    %s\n", action.Risk)
}

// logAction logs an action being executed
func (a *Applier) logAction(num, total int, action types.Action) {
	a.log("[%d/%d] EXECUTING: %s\n", num, total, action.Description)
	if a.verbose {
		a.log("  Target:  %s", a.formatTarget(action.Target))
		a.log("  Command: %s\n", action.Command)
		a.log("  Risk:    %s\n", action.Risk)
	}
}

// formatTarget formats a ResourceRef for display
func (a *Applier) formatTarget(ref types.ResourceRef) string {
	if ref.Namespace != "" {
		return fmt.Sprintf("%s/%s/%s\n", ref.Kind, ref.Namespace, ref.Name)
	}
	return fmt.Sprintf("%s/%s\n", ref.Kind, ref.Name)
}

// log writes to the output if not nil
func (a *Applier) log(format string, args ...interface{}) {
	if a.output != nil {
		fmt.Fprintf(a.output, format, args...)
	}
}

// calculateExitCode determines the exit code based on results
func (a *Applier) calculateExitCode(result *types.ApplyResult) int {
	if result.Failed == 0 && result.Skipped == 0 {
		return 0 // All succeeded
	}
	if result.Succeeded == 0 {
		return 1 // All failed or skipped
	}
	return 2 // Partial success
}
