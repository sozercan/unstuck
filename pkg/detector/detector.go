package detector

import (
	"context"

	"github.com/sozercan/unstuck/pkg/kube"
	"github.com/sozercan/unstuck/pkg/types"
)

// Detector is the interface for all resource detectors
type Detector interface {
	// Detect analyzes the target and returns a diagnosis report
	Detect(ctx context.Context, name string) (*types.DiagnosisReport, error)
}

// ResourceSpecificDetector is for resources that need namespace
type ResourceSpecificDetector interface {
	Detect(ctx context.Context, resourceType, name, namespace string) (*types.DiagnosisReport, error)
}

// DetectorFactory creates the appropriate detector for a target type
type DetectorFactory struct {
	client *kube.Client
}

// NewDetectorFactory creates a new detector factory
func NewDetectorFactory(client *kube.Client) *DetectorFactory {
	return &DetectorFactory{client: client}
}

// GetNamespaceDetector returns a namespace detector
func (f *DetectorFactory) GetNamespaceDetector() *NamespaceDetector {
	return NewNamespaceDetector(f.client)
}

// GetCRDDetector returns a CRD detector
func (f *DetectorFactory) GetCRDDetector() *CRDDetector {
	return NewCRDDetector(f.client)
}

// GetResourceDetector returns a generic resource detector
func (f *DetectorFactory) GetResourceDetector() *ResourceDetector {
	return NewResourceDetector(f.client)
}

// GetWebhookDetector returns a webhook detector
func (f *DetectorFactory) GetWebhookDetector() *WebhookDetector {
	return NewWebhookDetector(f.client)
}
