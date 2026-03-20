package cni

import (
	"fmt"
	"path/filepath"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/k8s"
)

const (
	// DefaultOVNKubeImage is the default image name for locally-built OVN-Kubernetes
	DefaultOVNKubeImage = "ovn-kube-fedora:dpu-sim"
)

// CNIManager manages CNI installations
type CNIManager struct {
	config *config.Config
	// k8sClient is an optional Kubernetes client for direct API access
	k8sClient *k8s.K8sClient
	// kubeconfigPath is the path to the kubeconfig file used for the
	// current cluster. It is set by SetKubeconfigFile and passed to
	// external tools (e.g. helm) that need cluster access.
	kubeconfigPath string
}

// NewCNIManager creates a new CNI manager with only a config.
// Use this for operations that do not require Kubernetes API access.
func NewCNIManager(cfg *config.Config) (*CNIManager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("failed to create CNI manager: config is nil")
	}
	return &CNIManager{config: cfg}, nil
}

// NewCNIManagerWithKubeconfig creates a new CNI manager with Kubernetes client from kubeconfig content
func NewCNIManagerWithKubeconfigFile(cfg *config.Config, kubeconfigPath string) (*CNIManager, error) {
	if kubeconfigPath == "" {
		return nil, fmt.Errorf("failed to create Kubernetes client from kubeconfig file: kubeconfig file path is empty")
	}

	absPath, err := filepath.Abs(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve kubeconfig path %s: %w", kubeconfigPath, err)
	}

	k8sClient, err := k8s.NewClientFromFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client from kubeconfig file: %w", err)
	}

	return &CNIManager{
		config:         cfg,
		k8sClient:      k8sClient,
		kubeconfigPath: absPath,
	}, nil
}

// SetKubeconfigFile replaces the Kubernetes client with one created from the
// given kubeconfig file path, allowing the same CNIManager to be reused
// across multiple clusters.
func (m *CNIManager) SetKubeconfigFile(kubeconfigPath string) error {
	if kubeconfigPath == "" {
		return fmt.Errorf("kubeconfig file path is empty")
	}
	absPath, err := filepath.Abs(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to resolve kubeconfig path %s: %w", kubeconfigPath, err)
	}
	k8sClient, err := k8s.NewClientFromFile(absPath)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client from kubeconfig file: %w", err)
	}
	m.k8sClient = k8sClient
	m.kubeconfigPath = absPath
	return nil
}

// K8sClient returns the underlying Kubernetes client for advanced operations
func (m *CNIManager) K8sClient() *k8s.K8sClient {
	return m.k8sClient
}

// DPUHostCredentials holds the credentials and network information for the
// DPU host cluster that the DPU cluster needs for cross-cluster access.
type DPUHostCredentials struct {
	APIServer   string
	Token       string
	CACert      string // base64-encoded CA certificate
	PodCIDR     string // e.g., "10.244.0.0/16/24" (with per-node prefix)
	ServiceCIDR string
}
