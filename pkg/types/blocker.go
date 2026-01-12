package types

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ResourceRef identifies a Kubernetes resource
type ResourceRef struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}

// String returns a human-readable representation of the resource
func (r ResourceRef) String() string {
	if r.Namespace != "" {
		return r.Kind + "/" + r.Namespace + "/" + r.Name
	}
	return r.Kind + "/" + r.Name
}

// Blocker represents a resource blocking deletion
type Blocker struct {
	ResourceRef
	Finalizers        []string               `json:"finalizers"`
	DeletionTimestamp *metav1.Time           `json:"deletionTimestamp,omitempty"`
	OwnerReferences   []metav1.OwnerReference `json:"ownerReferences,omitempty"`
	Age               time.Duration          `json:"age,omitempty"`
}

// IsTerminating returns true if the resource has a deletion timestamp
func (b Blocker) IsTerminating() bool {
	return b.DeletionTimestamp != nil
}

// BlockerStatus represents the status of a blocking resource
type BlockerStatus string

const (
	BlockerStatusStuck   BlockerStatus = "Stuck"
	BlockerStatusPending BlockerStatus = "Pending"
	BlockerStatusHealthy BlockerStatus = "Healthy"
)

// DiscoveryFailure represents an API discovery failure
type DiscoveryFailure struct {
	GroupVersion string `json:"groupVersion"`
	Resource     string `json:"resource,omitempty"`
	Error        string `json:"error"`
}

// WebhookInfo contains information about a blocking webhook
type WebhookInfo struct {
	Name           string `json:"name"`
	WebhookName    string `json:"webhookName"`
	Type           string `json:"type"` // validating or mutating
	Healthy        bool   `json:"healthy"`
	Error          string `json:"error,omitempty"`
	ServiceRef     string `json:"serviceRef,omitempty"`
	FailurePolicy  string `json:"failurePolicy,omitempty"`
}

// ControllerStatus represents the status of a controller/operator
type ControllerStatus struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Available bool   `json:"available"`
	Ready     bool   `json:"ready"`
	Message   string `json:"message,omitempty"`
}
