package kube

import (
	"fmt"

	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client wraps all Kubernetes clients needed by unstuck
type Client struct {
	// Kubernetes clientset for core resources
	Clientset kubernetes.Interface

	// Dynamic client for custom resources
	Dynamic dynamic.Interface

	// Discovery client for API discovery
	Discovery discovery.DiscoveryInterface

	// API extensions client for CRDs
	ApiExtensions apiextensionsclientset.Interface

	// REST config for raw API calls
	RestConfig *rest.Config
}

// NewClient creates a new Kubernetes client from kubeconfig
func NewClient(kubeconfig, context string) (*Client, error) {
	// Build config from kubeconfig file
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}

	configOverrides := &clientcmd.ConfigOverrides{}
	if context != "" {
		configOverrides.CurrentContext = context
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		configOverrides,
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	return NewClientFromConfig(config)
}

// NewClientFromConfig creates a new client from a REST config
func NewClientFromConfig(config *rest.Config) (*Client, error) {
	// Create clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	// Create dynamic client
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Create discovery client
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client: %w", err)
	}

	// Create API extensions client
	apiExtClient, err := apiextensionsclientset.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create API extensions client: %w", err)
	}

	return &Client{
		Clientset:     clientset,
		Dynamic:       dynamicClient,
		Discovery:     discoveryClient,
		ApiExtensions: apiExtClient,
		RestConfig:    config,
	}, nil
}

// NewClientForTesting creates a client for unit testing with provided interfaces
func NewClientForTesting(
	clientset kubernetes.Interface,
	dynamicClient dynamic.Interface,
	discoveryClient discovery.DiscoveryInterface,
	apiExtClient apiextensionsclientset.Interface,
) *Client {
	return &Client{
		Clientset:     clientset,
		Dynamic:       dynamicClient,
		Discovery:     discoveryClient,
		ApiExtensions: apiExtClient,
	}
}
