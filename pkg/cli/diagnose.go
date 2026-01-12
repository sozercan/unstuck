package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sozercan/unstuck/pkg/detector"
	"github.com/sozercan/unstuck/pkg/kube"
	"github.com/sozercan/unstuck/pkg/output"
	"github.com/sozercan/unstuck/pkg/types"
)

type diagnoseFlags struct {
	namespace string
	scope     string
}

var diagFlags diagnoseFlags

// NewDiagnoseCommand creates the diagnose command
func NewDiagnoseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diagnose <type> <name>",
		Short: "Diagnose a stuck Kubernetes resource",
		Long: `Diagnose why a Kubernetes resource is stuck in Terminating state.

Supported target types:
  namespace    Diagnose a namespace stuck in Terminating
  crd          Diagnose a CRD stuck in Terminating
  <resource>   Diagnose a specific resource (e.g., certificate, pod)

Examples:
  # Diagnose a stuck namespace
  unstuck diagnose namespace cert-manager

  # Diagnose a stuck CRD
  unstuck diagnose crd certificates.cert-manager.io

  # Diagnose a specific resource
  unstuck diagnose certificate my-cert -n cert-manager`,
		Args: cobra.MinimumNArgs(1),
		RunE: runDiagnose,
	}

	cmd.Flags().StringVarP(&diagFlags.namespace, "namespace", "n", "", "Namespace for resource targets")
	cmd.Flags().StringVar(&diagFlags.scope, "scope", "", "Label selector to limit scope")

	return cmd
}

func runDiagnose(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

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

	// Create the appropriate detector
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
		// Treat as a resource type (e.g., certificate, pod)
		if targetName == "" {
			return fmt.Errorf("resource name is required")
		}
		det := detector.NewResourceDetector(client)
		report, err = det.Detect(ctx, targetType, targetName, diagFlags.namespace)
	}

	if err != nil {
		return err
	}

	// Output the report
	outputFormat := GetOutputFormat()
	printer := output.NewPrinter(outputFormat, flags.Verbose)
	return printer.PrintDiagnosis(report)
}
