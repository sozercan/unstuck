package planner

import (
	"sort"
	"strings"

	"github.com/sozercan/unstuck/pkg/types"
)

// OrderByDependencies orders actions so that children are processed before parents
// This ensures that resources with owner references are deleted before their owners
func OrderByDependencies(actions []types.Action) []types.Action {
	if len(actions) == 0 {
		return actions
	}

	// Build a map of resource key to action for dependency lookup
	actionMap := make(map[string]*types.Action)
	for i := range actions {
		key := resourceKey(actions[i].Target)
		actionMap[key] = &actions[i]
	}

	// Build dependency graph - for now we'll use a simple sort by escalation level
	// and then by resource type (CRs before CRDs before Namespaces)
	sorted := make([]types.Action, len(actions))
	copy(sorted, actions)

	sort.SliceStable(sorted, func(i, j int) bool {
		// First, sort by escalation level
		if sorted[i].EscalationLevel != sorted[j].EscalationLevel {
			return sorted[i].EscalationLevel < sorted[j].EscalationLevel
		}

		// Within same escalation level, sort by resource type priority
		// Children (regular resources) come before parents (CRDs, Namespaces)
		pi := resourcePriority(sorted[i].Target.Kind)
		pj := resourcePriority(sorted[j].Target.Kind)
		if pi != pj {
			return pi < pj
		}

		// Same type, sort alphabetically by name for consistency
		return sorted[i].Target.Name < sorted[j].Target.Name
	})

	return sorted
}

// resourcePriority returns the deletion priority of a resource type
// Lower number = deleted first (children before parents)
func resourcePriority(kind string) int {
	switch strings.ToLower(kind) {
	// Leaf resources (deleted first)
	case "pod", "configmap", "secret", "service", "endpoint", "endpoints":
		return 10
	// Controller-managed resources
	case "deployment", "statefulset", "daemonset", "replicaset", "job", "cronjob":
		return 20
	// Custom resources (should be deleted before CRDs)
	case "certificate", "issuer", "clusterissuer":
		return 30
	// CRDs (deleted after their instances)
	case "customresourcedefinition":
		return 100
	// Namespaces (deleted last)
	case "namespace":
		return 200
	// Default for unknown resources
	default:
		return 50
	}
}

// resourceKey creates a unique key for a resource
func resourceKey(ref types.ResourceRef) string {
	if ref.Namespace != "" {
		return ref.Kind + "/" + ref.Namespace + "/" + ref.Name
	}
	return ref.Kind + "/" + ref.Name
}

// TopologicalSort performs a topological sort based on owner references
// This is used for more complex dependency graphs
func TopologicalSort(actions []types.Action, ownerRefs map[string][]string) []types.Action {
	if len(actions) == 0 {
		return actions
	}

	// If no owner refs provided, fall back to simple ordering
	if len(ownerRefs) == 0 {
		return OrderByDependencies(actions)
	}

	// Build in-degree map
	inDegree := make(map[string]int)
	dependents := make(map[string][]string)

	for _, action := range actions {
		key := resourceKey(action.Target)
		if _, exists := inDegree[key]; !exists {
			inDegree[key] = 0
		}
	}

	// Calculate in-degrees based on owner references
	for key, owners := range ownerRefs {
		for _, owner := range owners {
			if _, exists := inDegree[owner]; exists {
				inDegree[key]++
				dependents[owner] = append(dependents[owner], key)
			}
		}
	}

	// Perform Kahn's algorithm
	var queue []string
	for key, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, key)
		}
	}

	// Sort queue for deterministic output
	sort.Strings(queue)

	var sortedKeys []string
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		sortedKeys = append(sortedKeys, current)

		for _, dep := range dependents[current] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
		sort.Strings(queue)
	}

	// Reverse the order (children first, then parents)
	for i, j := 0, len(sortedKeys)-1; i < j; i, j = i+1, j-1 {
		sortedKeys[i], sortedKeys[j] = sortedKeys[j], sortedKeys[i]
	}

	// Map sorted keys back to actions
	actionMap := make(map[string]types.Action)
	for _, action := range actions {
		key := resourceKey(action.Target)
		actionMap[key] = action
	}

	var result []types.Action
	seen := make(map[string]bool)
	for _, key := range sortedKeys {
		if action, exists := actionMap[key]; exists && !seen[key] {
			result = append(result, action)
			seen[key] = true
		}
	}

	// Add any actions not in the sorted list (no dependencies)
	for _, action := range actions {
		key := resourceKey(action.Target)
		if !seen[key] {
			result = append(result, action)
		}
	}

	return result
}
