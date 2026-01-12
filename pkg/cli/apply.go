package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/sozercan/unstuck/pkg/applier"
	"github.com/sozercan/unstuck/pkg/detector"
	"github.com/sozercan/unstuck/pkg/kube"
	"github.com/sozercan/unstuck/pkg/output"
	"github.com/sozercan/unstuck/pkg/planner"
	"github.com/sozercan/unstuck/pkg/types"
)

type applyFlags struct {
	namespace       string
	scope           string
	maxEscalation   int
	allowForce      bool
	dryRun          bool
	yes             bool
	continueOnError bool
}

var appFlags applyFlags

// NewApplyCommand creates the apply command
func NewApplyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply <type> <name>",
		Short: "Apply remediation actions to unstick a resource",
		Long: `Apply remediation actions to a Kubernetes resource stuck in Terminating state.

This command first diagnoses the resource, generates a remediation plan,
and then executes the actions to unstick the resource.

⚠️  WARNING: This command modifies cluster state. Use with caution.

Escalation Levels:
  L0 (Informational)  - Read-only inspection (no changes)
  L1 (Clean Deletion) - Retry deletion with controller
  L2 (Finalizer)      - Remove finalizers from resources
  L3 (CRD Finalizer)  - Remove CRD cleanup finalizer (requires --allow-force)
  L4 (Force Finalize) - Force-finalize namespace (requires --allow-force)

Safety Features:
  - Use --dry-run to preview actions without applying
  - High-risk actions (L3+) require confirmation unless --yes is used
  - Use --max-escalation to limit the risk level

Examples:
  # Preview remediation actions (dry-run)
  unstuck apply namespace cert-manager --dry-run

  # Apply with default settings (max L2, prompts for confirmation)
  unstuck apply namespace cert-manager

  # Apply without confirmation prompts
  unstuck apply namespace cert-manager --yes

  # Force-finalize a namespace stuck due to missing CRDs
  unstuck apply namespace cert-manager --max-escalation=4 --allow-force --yes`,
		Args: cobra.MinimumNArgs(1),
		RunE: runApply,
	}

	cmd.Flags().StringVarP(&appFlags.namespace, "namespace", "n", "", "Namespace for resource targets")
	cmd.Flags().StringVar(&appFlags.scope, "scope", "", "Label selector to limit scope")
	cmd.Flags().IntVar(&appFlags.maxEscalation, "max-escalation", 2, "Maximum escalation level (0-4)")
	cmd.Flags().BoolVar(&appFlags.allowForce, "allow-force", false, "Allow Level 3-4 actions (CRD/namespace force)")
	cmd.Flags().BoolVar(&appFlags.dryRun, "dry-run", false, "Preview actions without applying")
	cmd.Flags().BoolVarP(&appFlags.yes, "yes", "y", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&appFlags.continueOnError, "continue-on-error", false, "Continue executing after errors")

	return cmd
}

func runApply(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Validate flags
	if appFlags.maxEscalation < 0 || appFlags.maxEscalation > 4 {
		return fmt.Errorf("max-escalation must be between 0 and 4")
	}
	if appFlags.maxEscalation >= 3 && !appFlags.allowForce {
		return fmt.Errorf("--allow-force is required for escalation level %d or higher", appFlags.maxEscalation)
	}

	// Parse arguments
	targetType := args[0]
	var targetName string
	if len(args) > 1 {
		targetName = args[1]
	}

	// Build Kubernetes client
	flags := GetGlobalFlags()
	client, err := kube.NewClient(flags.Kubeconfig, flags.Context)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// First, run diagnosis to get the current state
	var report *types.DiagnosisReport

	switch targetType {
	case "namespace", "ns":
		if targetName == "" {
			return fmt.Errorf("namespace name is required")
		}
		det := detector.NewNamespaceDetector(client)
		report, err = det.Detect(ctx, targetName)

	case "crd", "customresourcedefinition":
		if targetName == "" {
			return fmt.Errorf("CRD name is required")
		}
		det := detector.NewCRDDetector(client)
		report, err = det.Detect(ctx, targetName)

	default:
		// Treat as a resource type
		if targetName == "" {
			return fmt.Errorf("resource name is required")
		}
		det := detector.NewResourceDetector(client)
		report, err = det.Detect(ctx, targetType, targetName, appFlags.namespace)
	}

	if err != nil {
		return err
	}

	// Print diagnosis summary if verbose
	if flags.Verbose {
		outputFormat := GetOutputFormat()
		printer := output.NewPrinter(outputFormat, flags.Verbose)
		if err := printer.Print(report); err != nil {
			return err
		}
		fmt.Println() // Add spacing
	}

	// Generate the plan
	p := planner.NewPlanner(planner.Options{
		MaxEscalation: types.EscalationLevel(appFlags.maxEscalation),
		AllowForce:    appFlags.allowForce,
	})

	plan, err := p.Plan(report)
	if err != nil {
		return fmt.Errorf("failed to generate plan: %w", err)
	}

	// Check if there are any actions to apply
	if len(plan.Actions) == 0 {
		fmt.Println("✅ No remediation actions needed - resource is healthy or will delete naturally")
		return nil
	}

	// Print plan summary
	fmt.Printf("📋 Remediation Plan: %d actions (max risk: %s)\n\n", len(plan.Actions), plan.RiskLevel)

	// Apply the plan
	app := applier.NewApplier(client, applier.Options{
		DryRun:          appFlags.dryRun,
		ContinueOnError: appFlags.continueOnError,
		AutoConfirm:     appFlags.yes,
		Verbose:         flags.Verbose,
		Output:          os.Stdout,
	})

	result, err := app.Apply(ctx, plan)
	if err != nil {
		return fmt.Errorf("failed to apply plan: %w", err)
	}

	// Print result summary
	fmt.Println()
	outputFormat := GetOutputFormat()
	printer := output.NewPrinter(outputFormat, flags.Verbose)
	if err := printer.PrintApplyResult(result); err != nil {
		return err
	}

	// Exit with appropriate code
	if result.ExitCode != 0 {
		os.Exit(result.ExitCode)
	}

	return nil
}
