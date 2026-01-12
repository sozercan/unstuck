package planner

import (
	"fmt"

	"github.com/sozercan/unstuck/pkg/types"
)

// Planner generates remediation plans from diagnosis reports
type Planner struct {
	maxEscalation types.EscalationLevel
	allowForce    bool
}

// Options configures the planner behavior
type Options struct {
	MaxEscalation types.EscalationLevel
	AllowForce    bool
}

// NewPlanner creates a new Planner with the given options
func NewPlanner(opts Options) *Planner {
	return &Planner{
		maxEscalation: opts.MaxEscalation,
		allowForce:    opts.AllowForce,
	}
}

// Plan generates a remediation plan from a diagnosis report
func (p *Planner) Plan(diagnosis *types.DiagnosisReport) (*types.Plan, error) {
	if diagnosis == nil {
		return nil, fmt.Errorf("diagnosis report is nil")
	}

	plan := &types.Plan{
		Target:        diagnosis.Target,
		MaxEscalation: p.maxEscalation,
		Actions:       []types.Action{},
		Commands:      []string{},
	}

	// If healthy, no actions needed
	if diagnosis.IsHealthy() {
		return plan, nil
	}

	var actions []types.Action

	// Level 0: Informational actions (always included)
	actions = append(actions, p.infoActions(diagnosis)...)

	// Level 1: Clean path actions if controller is available
	if p.maxEscalation >= types.EscalationClean {
		if hasAvailableController(diagnosis) {
			actions = append(actions, p.cleanPathActions(diagnosis)...)
		}
	}

	// Level 2: Finalizer removal actions for blockers
	if p.maxEscalation >= types.EscalationFinalizer {
		for _, blocker := range diagnosis.Blockers {
			actions = append(actions, p.finalizerRemovalAction(blocker, diagnosis.Target.Namespace))
		}
	}

	// Level 3: CRD finalizer removal (requires force)
	if p.maxEscalation >= types.EscalationCRD && p.allowForce {
		if diagnosis.TargetType == types.TargetTypeCRD {
			actions = append(actions, p.crdFinalizerAction(diagnosis.Target))
		}
	}

	// Level 4: Force-finalize namespace (requires force)
	if p.maxEscalation >= types.EscalationForce && p.allowForce {
		if diagnosis.TargetType == types.TargetTypeNamespace && diagnosis.HasDiscoveryFailures() {
			actions = append(actions, p.forceFinalize(diagnosis.Target.Name))
		}
	}

	// Order actions by dependencies (children before parents)
	orderedActions := OrderByDependencies(actions)

	// Assign action IDs
	for i := range orderedActions {
		orderedActions[i].ID = fmt.Sprintf("action-%03d", i+1)
	}

	plan.Actions = orderedActions
	plan.RiskLevel = CalculateRiskLevel(orderedActions)
	plan.Commands = GenerateCommands(orderedActions)

	return plan, nil
}

// infoActions returns read-only informational actions
func (p *Planner) infoActions(diagnosis *types.DiagnosisReport) []types.Action {
	var actions []types.Action

	// Add inspect action for the target
	actions = append(actions, types.Action{
		Type:            types.ActionInspect,
		EscalationLevel: types.EscalationInfo,
		Description:     fmt.Sprintf("Inspect %s %q", diagnosis.TargetType, diagnosis.Target.Name),
		Target:          diagnosis.Target,
		Operation:       "inspect",
		Risk:            types.RiskNone,
		RequiresForce:   false,
		ExpectedResult:  "Gather current state information",
	})

	// Add list action for remaining resources
	if len(diagnosis.Blockers) > 0 {
		actions = append(actions, types.Action{
			Type:            types.ActionList,
			EscalationLevel: types.EscalationInfo,
			Description:     fmt.Sprintf("List %d blocking resources", len(diagnosis.Blockers)),
			Target:          diagnosis.Target,
			Operation:       "list-blockers",
			Risk:            types.RiskNone,
			RequiresForce:   false,
			ExpectedResult:  "Enumerate resources preventing deletion",
		})
	}

	return actions
}

// cleanPathActions returns actions for clean deletion when controller is available
func (p *Planner) cleanPathActions(diagnosis *types.DiagnosisReport) []types.Action {
	var actions []types.Action

	// Wait for controller to process finalizers
	actions = append(actions, types.Action{
		Type:            types.ActionWait,
		EscalationLevel: types.EscalationClean,
		Description:     "Wait for controller to process finalizers",
		Target:          diagnosis.Target,
		Operation:       "wait-controller",
		Risk:            types.RiskLow,
		RequiresForce:   false,
		ExpectedResult:  "Controller removes finalizers naturally",
	})

	return actions
}

// finalizerRemovalAction creates an action to remove finalizers from a blocker
func (p *Planner) finalizerRemovalAction(blocker types.Blocker, namespace string) types.Action {
	ns := blocker.Namespace
	if ns == "" {
		ns = namespace
	}

	target := types.ResourceRef{
		Kind:       blocker.Kind,
		APIVersion: blocker.APIVersion,
		Namespace:  ns,
		Name:       blocker.Name,
	}

	return types.Action{
		Type:            types.ActionPatch,
		EscalationLevel: types.EscalationFinalizer,
		Description:     fmt.Sprintf("Remove finalizers from %s", target.String()),
		Target:          target,
		Operation:       "remove-finalizers",
		Command:         generatePatchCommand(target),
		Risk:            types.RiskMedium,
		RequiresForce:   false,
		ExpectedResult:  "Finalizers removed, object deletion proceeds",
	}
}

// crdFinalizerAction creates an action to remove CRD cleanup finalizer
func (p *Planner) crdFinalizerAction(target types.ResourceRef) types.Action {
	return types.Action{
		Type:            types.ActionPatch,
		EscalationLevel: types.EscalationCRD,
		Description:     fmt.Sprintf("Remove cleanup finalizer from CRD %s", target.Name),
		Target: types.ResourceRef{
			Kind:       "CustomResourceDefinition",
			APIVersion: "apiextensions.k8s.io/v1",
			Name:       target.Name,
		},
		Operation:      "remove-crd-finalizer",
		Command:        generateCRDPatchCommand(target.Name),
		Risk:           types.RiskHigh,
		RequiresForce:  true,
		ExpectedResult: "CRD cleanup finalizer removed, CR data may be orphaned",
	}
}

// forceFinalize creates an action to force-finalize a namespace
func (p *Planner) forceFinalize(namespaceName string) types.Action {
	return types.Action{
		Type:            types.ActionFinalize,
		EscalationLevel: types.EscalationForce,
		Description:     fmt.Sprintf("Force-finalize namespace %s", namespaceName),
		Target: types.ResourceRef{
			Kind:       "Namespace",
			APIVersion: "v1",
			Name:       namespaceName,
		},
		Operation:      "force-finalize",
		Command:        generateForceFinalize(namespaceName),
		Risk:           types.RiskCritical,
		RequiresForce:  true,
		ExpectedResult: "Namespace deleted, remaining resources abandoned",
	}
}

// hasAvailableController checks if there's an available controller for the diagnosis
func hasAvailableController(diagnosis *types.DiagnosisReport) bool {
	for _, ctrl := range diagnosis.Controllers {
		if ctrl.Available && ctrl.Ready {
			return true
		}
	}
	return false
}

// generatePatchCommand generates a kubectl patch command for finalizer removal
func generatePatchCommand(target types.ResourceRef) string {
	kind := target.Kind
	if kind == "" {
		kind = "resource"
	}

	cmd := fmt.Sprintf("kubectl patch %s %s", kind, target.Name)
	if target.Namespace != "" {
		cmd += fmt.Sprintf(" -n %s", target.Namespace)
	}
	cmd += " -p '{\"metadata\":{\"finalizers\":null}}' --type=merge"
	return cmd
}

// generateCRDPatchCommand generates a kubectl patch command for CRD finalizer removal
func generateCRDPatchCommand(crdName string) string {
	return fmt.Sprintf("kubectl patch crd %s -p '{\"metadata\":{\"finalizers\":null}}' --type=merge", crdName)
}

// generateForceFinalize generates the kubectl command sequence for force-finalizing a namespace
func generateForceFinalize(namespaceName string) string {
	return fmt.Sprintf(
		"kubectl get namespace %s -o json | jq '.spec.finalizers = []' | kubectl replace --raw \"/api/v1/namespaces/%s/finalize\" -f -",
		namespaceName, namespaceName,
	)
}
