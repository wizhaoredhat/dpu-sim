package cni

import (
	"fmt"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/k8s"
)

// CNIType represents the type of CNI
type CNIType string

const (
	CNIFlannel       CNIType = "flannel"
	CNIOVNKubernetes CNIType = "ovn-kubernetes"
	CNIKindnet       CNIType = "kindnet"

	// DefaultOVNKubeImage is the default image name for locally-built OVN-Kubernetes
	DefaultOVNKubeImage = "ovn-kube-fedora:dpu-sim"
)

// CNIManager manages CNI installations
type CNIManager struct {
	config *config.Config
	// k8sClient is an optional Kubernetes client for direct API access
	k8sClient *k8s.K8sClient
}

// NewCNIManager creates a new CNI manager with only a config.
// Use this for operations that do not require Kubernetes API access (e.g. RebuildCNIImage).
func NewCNIManager(cfg *config.Config) (*CNIManager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("failed to create CNI manager: config is nil")
	}
	return &CNIManager{config: cfg}, nil
}

// NewCNIManagerWithKubeconfig creates a new CNI manager with Kubernetes client from kubeconfig content
func NewCNIManagerWithKubeconfig(cfg *config.Config, kubeconfigContent string) (*CNIManager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("failed to create CNI manager: config is nil")
	}

	if kubeconfigContent == "" {
		return nil, fmt.Errorf("failed to create Kubernetes client from kubeconfig content: kubeconfig content is empty")
	}

	k8sClient, err := k8s.NewClient(kubeconfigContent)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client from kubeconfig content: %w", err)
	}

	return &CNIManager{
		config:    cfg,
		k8sClient: k8sClient,
	}, nil
}

// NewCNIManagerWithKubeconfig creates a new CNI manager with Kubernetes client from kubeconfig content
func NewCNIManagerWithKubeconfigFile(cfg *config.Config, kubeconfigPath string) (*CNIManager, error) {
	if kubeconfigPath == "" {
		return nil, fmt.Errorf("failed to create Kubernetes client from kubeconfig file: kubeconfig file path is empty")
	}

	k8sClient, err := k8s.NewClientFromFile(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client from kubeconfig file: %w", err)
	}

	return &CNIManager{
		config:    cfg,
		k8sClient: k8sClient,
	}, nil
}

// SetKubeconfigFile replaces the Kubernetes client with one created from the
// given kubeconfig file path, allowing the same CNIManager to be reused
// across multiple clusters.
func (m *CNIManager) SetKubeconfigFile(kubeconfigPath string) error {
	if kubeconfigPath == "" {
		return fmt.Errorf("kubeconfig file path is empty")
	}
	k8sClient, err := k8s.NewClientFromFile(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client from kubeconfig file: %w", err)
	}
	m.k8sClient = k8sClient
	return nil
}

// K8sClient returns the underlying Kubernetes client for advanced operations
func (m *CNIManager) K8sClient() *k8s.K8sClient {
	return m.k8sClient
}
