package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"k8s.io/client-go/util/homedir"
)

// GlobalFlags contains flags shared across all commands
type GlobalFlags struct {
	Kubeconfig string
	Context    string
	Output     string
	Verbose    bool
	Timeout    string
}

var globalFlags GlobalFlags

// NewRootCommand creates the root cobra command
func NewRootCommand(version, commit, date string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unstuck",
		Short: "Diagnose and remediate Kubernetes resources stuck in Terminating state",
		Long: `Unstuck is a CLI tool to diagnose and remediate Kubernetes resources 
stuck in Terminating state due to unsatisfiable finalizers, missing controllers, 
deleted CRDs, or blocking webhooks.

Examples:
  # Diagnose a stuck namespace
  unstuck diagnose namespace cert-manager

  # Diagnose a stuck CRD
  unstuck diagnose crd certificates.cert-manager.io

  # Diagnose a specific resource
  unstuck diagnose certificate my-cert -n cert-manager`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
	}

	// Global flags
	defaultKubeconfig := ""
	if home := homedir.HomeDir(); home != "" {
		defaultKubeconfig = filepath.Join(home, ".kube", "config")
	}
	if envKubeconfig := os.Getenv("KUBECONFIG"); envKubeconfig != "" {
		defaultKubeconfig = envKubeconfig
	}

	cmd.PersistentFlags().StringVar(&globalFlags.Kubeconfig, "kubeconfig", defaultKubeconfig, "Path to kubeconfig file")
	cmd.PersistentFlags().StringVar(&globalFlags.Context, "context", "", "Kubernetes context to use")
	cmd.PersistentFlags().StringVarP(&globalFlags.Output, "output", "o", "", "Output format: text, json, yaml (auto-detects TTY)")
	cmd.PersistentFlags().BoolVarP(&globalFlags.Verbose, "verbose", "v", false, "Verbose output")
	cmd.PersistentFlags().StringVar(&globalFlags.Timeout, "timeout", "5m", "Overall operation timeout")

	// Add subcommands
	cmd.AddCommand(NewDiagnoseCommand())
	cmd.AddCommand(NewPlanCommand())
	cmd.AddCommand(NewApplyCommand())

	return cmd
}

// GetOutputFormat returns the output format, auto-detecting if not specified
func GetOutputFormat() string {
	if globalFlags.Output != "" {
		return globalFlags.Output
	}
	// Auto-detect based on TTY
	if isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd()) {
		return "text"
	}
	return "json"
}

// GetGlobalFlags returns the global flags
func GetGlobalFlags() GlobalFlags {
	return globalFlags
}
