package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/rodaine/table"

	"github.com/sozercan/unstuck/pkg/types"
)

// Printer handles output formatting
type Printer struct {
	format  string
	verbose bool
	out     io.Writer
}

// NewPrinter creates a new printer
func NewPrinter(format string, verbose bool) *Printer {
	return &Printer{
		format:  format,
		verbose: verbose,
		out:     os.Stdout,
	}
}

// SetOutput sets the output writer
func (p *Printer) SetOutput(out io.Writer) {
	p.out = out
}

// PrintDiagnosis outputs a diagnosis report
func (p *Printer) PrintDiagnosis(report *types.DiagnosisReport) error {
	switch p.format {
	case "json":
		return p.printJSON(report)
	case "yaml":
		return p.printYAML(report)
	default:
		return p.printText(report)
	}
}

// printJSON outputs the report as JSON
func (p *Printer) printJSON(report *types.DiagnosisReport) error {
	encoder := json.NewEncoder(p.out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

// printYAML outputs the report as YAML (simplified, using JSON for now)
func (p *Printer) printYAML(report *types.DiagnosisReport) error {
	return p.printJSON(report)
}

// printText outputs the report as human-readable text
func (p *Printer) printText(report *types.DiagnosisReport) error {
	bold := color.New(color.Bold)
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)
	red := color.New(color.FgRed)
	cyan := color.New(color.FgCyan)

	fmt.Fprintln(p.out)
	bold.Fprintf(p.out, "DIAGNOSIS: %s %q\n", report.Target.Kind, report.Target.Name)
	fmt.Fprintln(p.out, strings.Repeat("━", 68))
	fmt.Fprintln(p.out)

	fmt.Fprintf(p.out, "%-16s", "Status:")
	if report.IsHealthy() {
		green.Fprintf(p.out, "%s ✓\n", report.Status)
	} else {
		terminatingFor := ""
		if report.TerminatingFor != "" {
			terminatingFor = fmt.Sprintf(" (since %s ago)", report.TerminatingFor)
		}
		red.Fprintf(p.out, "%s%s\n", report.Status, terminatingFor)
	}

	if report.IsHealthy() {
		fmt.Fprintln(p.out)
		green.Fprintf(p.out, "The %s %q is not in Terminating state.\n",
			strings.ToLower(report.Target.Kind), report.Target.Name)
		fmt.Fprintln(p.out, "No remediation needed.")
		fmt.Fprintln(p.out)
		return nil
	}

	if report.RootCause != "" {
		fmt.Fprintf(p.out, "%-16s%s\n", "Root Cause:", report.RootCause)
	}

	if len(report.Finalizers) > 0 {
		fmt.Fprintf(p.out, "%-16s%s\n", "Finalizers:", strings.Join(report.Finalizers, ", "))
	}

	fmt.Fprintln(p.out)

	if len(report.Blockers) > 0 {
		bold.Fprintf(p.out, "BLOCKERS (%d found)\n", len(report.Blockers))

		headerFmt := color.New(color.FgHiWhite, color.Bold).SprintfFunc()
		tbl := table.New("#", "Resource", "Finalizer", "Status")
		tbl.WithHeaderFormatter(headerFmt)
		tbl.WithWriter(p.out)

		for i, b := range report.Blockers {
			status := "Stuck"
			if !b.IsTerminating() {
				status = "Pending"
			}

			finalizer := "-"
			if len(b.Finalizers) > 0 {
				finalizer = b.Finalizers[0]
				if len(b.Finalizers) > 1 {
					finalizer += fmt.Sprintf(" (+%d)", len(b.Finalizers)-1)
				}
			}

			resourceName := b.Kind + "/" + b.Name
			if b.Namespace != "" && b.Namespace != report.Target.Name {
				resourceName = b.Kind + "/" + b.Namespace + "/" + b.Name
			}

			tbl.AddRow(i+1, resourceName, finalizer, status)
		}
		tbl.Print()
		fmt.Fprintln(p.out)
	}

	if len(report.DiscoveryFailures) > 0 {
		yellow.Fprintln(p.out, "DISCOVERY FAILURES")
		for _, f := range report.DiscoveryFailures {
			fmt.Fprintf(p.out, "• %s - %s\n", f.GroupVersion, f.Error)
		}
		fmt.Fprintln(p.out)
	}

	if p.verbose && len(report.Conditions) > 0 {
		cyan.Fprintln(p.out, "CONDITIONS")
		for _, c := range report.Conditions {
			if c.Status == "True" {
				fmt.Fprintf(p.out, "• %s: %s\n", c.Type, c.Status)
				if c.Message != "" {
					fmt.Fprintf(p.out, "  Message: %s\n", truncate(c.Message, 100))
				}
			}
		}
		fmt.Fprintln(p.out)
	}

	if report.InstanceCount > 0 {
		bold.Fprintf(p.out, "REMAINING INSTANCES (%d across %d namespaces)\n",
			report.InstanceCount, len(report.InstancesByNS))
		for ns, count := range report.InstancesByNS {
			fmt.Fprintf(p.out, "• %s: %d instances\n", ns, count)
		}
		fmt.Fprintln(p.out)
	}

	if len(report.Recommendations) > 0 {
		cyan.Fprintln(p.out, "RECOMMENDATION")
		for _, rec := range report.Recommendations {
			fmt.Fprintf(p.out, "%s\n", rec)
		}
		fmt.Fprintln(p.out)
	}

	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// PrintPlan outputs a remediation plan
func (p *Printer) PrintPlan(report *types.DiagnosisReport, plan *types.Plan) error {
	switch p.format {
	case "json":
		return p.printPlanJSON(report, plan)
	case "yaml":
		return p.printPlanJSON(report, plan)
	default:
		return p.printPlanText(report, plan)
	}
}

// printPlanJSON outputs the plan as JSON
func (p *Printer) printPlanJSON(report *types.DiagnosisReport, plan *types.Plan) error {
	// Combine diagnosis and plan into a single output
	output := struct {
		Target    types.ResourceRef      `json:"target"`
		Diagnosis *types.DiagnosisReport `json:"diagnosis"`
		Plan      *types.Plan            `json:"plan"`
		Commands  []string               `json:"commands,omitempty"`
	}{
		Target:    plan.Target,
		Diagnosis: report,
		Plan:      plan,
		Commands:  plan.Commands,
	}

	encoder := json.NewEncoder(p.out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

// printPlanText outputs the plan as human-readable text
func (p *Printer) printPlanText(report *types.DiagnosisReport, plan *types.Plan) error {
	bold := color.New(color.Bold)
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)
	red := color.New(color.FgRed)
	cyan := color.New(color.FgCyan)

	fmt.Fprintln(p.out)
	bold.Fprintf(p.out, "REMEDIATION PLAN: %s %q\n", plan.Target.Kind, plan.Target.Name)
	fmt.Fprintln(p.out, strings.Repeat("━", 68))
	fmt.Fprintln(p.out)

	// If healthy, no plan needed
	if report.IsHealthy() {
		green.Fprintf(p.out, "The %s %q is not in Terminating state.\n",
			strings.ToLower(plan.Target.Kind), plan.Target.Name)
		fmt.Fprintln(p.out, "No remediation needed.")
		fmt.Fprintln(p.out)
		return nil
	}

	// Risk level
	fmt.Fprintf(p.out, "%-16s", "Risk Level:")
	switch plan.RiskLevel {
	case types.RiskNone:
		green.Fprintf(p.out, "NONE\n")
	case types.RiskLow:
		green.Fprintf(p.out, "LOW\n")
	case types.RiskMedium:
		yellow.Fprintf(p.out, "MEDIUM\n")
	case types.RiskHigh:
		red.Fprintf(p.out, "HIGH\n")
	case types.RiskCritical:
		red.Fprintf(p.out, "CRITICAL\n")
	}

	fmt.Fprintf(p.out, "%-16s%s\n", "Max Escalation:", plan.MaxEscalation.String())
	fmt.Fprintln(p.out)

	// Check for force actions warning
	hasForce := false
	for _, action := range plan.Actions {
		if action.RequiresForce {
			hasForce = true
			break
		}
	}
	if hasForce {
		yellow.Fprintln(p.out, "⚠️  WARNING: This plan includes Level 3+ actions that require --allow-force")
		fmt.Fprintln(p.out)
	}

	// Actions table
	if len(plan.Actions) == 0 {
		fmt.Fprintln(p.out, "No actions required.")
		fmt.Fprintln(p.out)
		return nil
	}

	bold.Fprintf(p.out, "ACTIONS (%d steps)\n", len(plan.Actions))

	headerFmt := color.New(color.FgHiWhite, color.Bold).SprintfFunc()
	tbl := table.New("#", "Level", "Target", "Action", "Risk")
	tbl.WithHeaderFormatter(headerFmt)
	tbl.WithWriter(p.out)

	for i, action := range plan.Actions {
		level := fmt.Sprintf("L%d", action.EscalationLevel)
		target := action.Target.String()
		if len(target) > 40 {
			target = target[:37] + "..."
		}

		risk := string(action.Risk)
		if action.RequiresForce {
			risk += " (force req'd)"
		}

		tbl.AddRow(i+1, level, target, string(action.Type), risk)
	}
	tbl.Print()
	fmt.Fprintln(p.out)

	// Show commands if available
	if len(plan.Commands) > 0 && p.verbose {
		cyan.Fprintln(p.out, "KUBECTL COMMANDS")
		for i, cmd := range plan.Commands {
			fmt.Fprintf(p.out, "# Step %d\n", i+1)
			fmt.Fprintf(p.out, "%s\n\n", cmd)
		}
	}

	// Usage instructions
	fmt.Fprintf(p.out, "To execute: unstuck apply %s %s\n", report.TargetType, plan.Target.Name)
	fmt.Fprintf(p.out, "To dry-run: unstuck apply %s %s --dry-run\n", report.TargetType, plan.Target.Name)
	fmt.Fprintln(p.out)

	return nil
}

// Print outputs a diagnosis report (alias for PrintDiagnosis)
func (p *Printer) Print(report *types.DiagnosisReport) error {
	return p.PrintDiagnosis(report)
}

// PrintApplyResult outputs the result of applying a plan
func (p *Printer) PrintApplyResult(result *types.ApplyResult) error {
	switch p.format {
	case "json":
		return p.printApplyResultJSON(result)
	case "yaml":
		return p.printApplyResultJSON(result)
	default:
		return p.printApplyResultText(result)
	}
}

// printApplyResultJSON outputs the result as JSON
func (p *Printer) printApplyResultJSON(result *types.ApplyResult) error {
	encoder := json.NewEncoder(p.out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// printApplyResultText outputs the result as human-readable text
func (p *Printer) printApplyResultText(result *types.ApplyResult) error {
	bold := color.New(color.Bold)
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)
	red := color.New(color.FgRed)

	bold.Fprintf(p.out, "RESULT SUMMARY\n")
	fmt.Fprintln(p.out, strings.Repeat("━", 40))
	fmt.Fprintln(p.out)

	duration := result.EndTime.Sub(result.StartTime).Round(100 * 1000000) // Round to 100ms
	fmt.Fprintf(p.out, "%-16s%d\n", "Total Actions:", result.TotalActions)
	green.Fprintf(p.out, "%-16s%d\n", "Succeeded:", result.Succeeded)
	if result.Failed > 0 {
		red.Fprintf(p.out, "%-16s%d\n", "Failed:", result.Failed)
	} else {
		fmt.Fprintf(p.out, "%-16s%d\n", "Failed:", result.Failed)
	}
	if result.Skipped > 0 {
		yellow.Fprintf(p.out, "%-16s%d\n", "Skipped:", result.Skipped)
	} else {
		fmt.Fprintf(p.out, "%-16s%d\n", "Skipped:", result.Skipped)
	}
	fmt.Fprintf(p.out, "%-16s%s\n", "Duration:", duration)
	fmt.Fprintln(p.out)

	// Overall status
	switch result.ExitCode {
	case 0:
		green.Fprintln(p.out, "✅ All actions completed successfully")
	case 1:
		red.Fprintln(p.out, "❌ All actions failed")
	case 2:
		yellow.Fprintln(p.out, "⚠️  Partial success - some actions failed")
	}

	// Show failed actions if verbose
	if p.verbose && result.Failed > 0 {
		fmt.Fprintln(p.out)
		red.Fprintln(p.out, "FAILED ACTIONS")
		for _, ar := range result.Actions {
			if !ar.Success {
				fmt.Fprintf(p.out, "• %s: %s\n", ar.Action.Description, ar.Error)
			}
		}
	}

	fmt.Fprintln(p.out)
	return nil
}

// PrintWebhookReport outputs a webhook health report
func (p *Printer) PrintWebhookReport(webhooks []types.WebhookInfo) error {
	switch p.format {
	case "json":
		encoder := json.NewEncoder(p.out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(webhooks)
	default:
		return p.printWebhookReportText(webhooks)
	}
}

// printWebhookReportText outputs webhook status as human-readable text
func (p *Printer) printWebhookReportText(webhooks []types.WebhookInfo) error {
	bold := color.New(color.Bold)
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)
	red := color.New(color.FgRed)

	if len(webhooks) == 0 {
		fmt.Fprintln(p.out, "No applicable webhooks found.")
		return nil
	}

	// Count healthy/unhealthy
	healthy := 0
	unhealthy := 0
	for _, w := range webhooks {
		if w.Healthy {
			healthy++
		} else {
			unhealthy++
		}
	}

	bold.Fprintf(p.out, "WEBHOOK STATUS\n")
	fmt.Fprintln(p.out, strings.Repeat("━", 68))
	fmt.Fprintln(p.out)

	fmt.Fprintf(p.out, "%-16s%d\n", "Total:", len(webhooks))
	green.Fprintf(p.out, "%-16s%d\n", "Healthy:", healthy)
	if unhealthy > 0 {
		red.Fprintf(p.out, "%-16s%d\n", "Unhealthy:", unhealthy)
	}
	fmt.Fprintln(p.out)

	// Print table of webhooks
	headerFmt := color.New(color.FgHiWhite, color.Bold).SprintfFunc()
	tbl := table.New("Name", "Type", "Status", "Policy", "Service")
	tbl.WithHeaderFormatter(headerFmt)
	tbl.WithWriter(p.out)

	for _, w := range webhooks {
		status := "✓ Healthy"
		if !w.Healthy {
			status = "✗ " + w.Error
			if len(status) > 30 {
				status = status[:27] + "..."
			}
		}

		serviceRef := w.ServiceRef
		if serviceRef == "" {
			serviceRef = "(URL)"
		}

		tbl.AddRow(w.Name, w.Type, status, w.FailurePolicy, serviceRef)
	}
	tbl.Print()
	fmt.Fprintln(p.out)

	// Show guidance for unhealthy webhooks
	if unhealthy > 0 {
		yellow.Fprintln(p.out, "⚠️  UNHEALTHY WEBHOOKS MAY BLOCK OPERATIONS")
		fmt.Fprintln(p.out)
		fmt.Fprintln(p.out, "Webhooks with FailurePolicy=Fail will block API operations when unhealthy.")
		fmt.Fprintln(p.out, "Consider checking the webhook service health or temporarily disabling the webhook.")
		fmt.Fprintln(p.out)
	}

	return nil
}
