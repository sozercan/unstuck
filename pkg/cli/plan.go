package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sozercan/unstuck/pkg/detector"
	"github.com/sozercan/unstuck/pkg/kube"
	"github.com/sozercan/unstuck/pkg/output"
	"github.com/sozercan/unstuck/pkg/planner"
	"github.com/sozercan/unstuck/pkg/types"
)

type planFlags struct {
	namespace     string
	scope         string
	maxEscalation int
	allowForce    bool
}

var plFlags planFlags

// NewPlanCommand creates the plan command
func NewPlanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan <type> <name>",
		Short: "Generate a remediation plan for a stuck resource",
		Long: `Generate a remediation plan for a Kubernetes resource stuck in Terminating state.

The plan shows the actions that would be taken to unstick the resource,
ordered by dependencies and escalation level.

Escalation Levels:
  L0 (Informational)  - Read-only inspection
  L1 (Clean Deletion) - Retry deletion with controller
  L2 (Finalizer)      - Remove finalizers from resources
  L3 (CRD Finalizer)  - Remove CRD cleanup finalizer (requires --allow-force)
  L4 (Force Finalize) - Force-finalize namespace (requires --allow-force)

Examples:
  # Generate plan with default max escalation (Level 2)
  unstuck plan namespace cert-manager

  # Limit to informational only
  unstuck plan namespace cert-manager --max-escalation=0

  # Allow force-level actions in plan
  unstuck plan namespace cert-manager --max-escalation=4 --allow-force

  # Plan for CRD
  unstuck plan crd certificates.cert-manager.io`,
		Args: cobra.MinimumNArgs(1),
		RunE: runPlan,
	}

	cmd.Flags().StringVarP(&plFlags.namespace, "namespace", "n", "", "Namespace for resource targets")
	cmd.Flags().StringVar(&plFlags.scope, "scope", "", "Label selector to limit scope")
	cmd.Flags().IntVar(&plFlags.maxEscalation, "max-escalation", 2, "Maximum escalation level (0-4)")
	cmd.Flags().BoolVar(&plFlags.allowForce, "allow-force", false, "Allow Level 3-4 actions (CRD/namespace force)")

	return cmd
}

func runPlan(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Validate flags
	if plFlags.maxEscalation < 0 || plFlags.maxEscalation > 4 {
		return fmt.Errorf("max-escalation must be between 0 and 4")
	}
	if plFlags.maxEscalation >= 3 && !plFlags.allowForce {
		return fmt.Errorf("--allow-force is required for escalation level %d or higher", plFlags.maxEscalation)
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
		report, err = det.Detect(ctx, targetType, targetName, plFlags.namespace)
	}

	if err != nil {
		return err
	}

	// Generate the plan
	p := planner.NewPlanner(planner.Options{
		MaxEscalation: types.EscalationLevel(plFlags.maxEscalation),
		AllowForce:    plFlags.allowForce,
	})

	plan, err := p.Plan(report)
	if err != nil {
		return fmt.Errorf("failed to generate plan: %w", err)
	}

	// Output the plan
	outputFormat := GetOutputFormat()
	printer := output.NewPrinter(outputFormat, flags.Verbose)
	return printer.PrintPlan(report, plan)
}
