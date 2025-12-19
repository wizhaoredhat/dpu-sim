package cni

import (
	"fmt"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/k8s"
	"github.com/wizhao/dpu-sim/pkg/ssh"
)

// CNIType represents the type of CNI
type CNIType string

const (
	CNIFlannel       CNIType = "flannel"
	CNIOVNKubernetes CNIType = "ovn-kubernetes"
	CNIKindnet       CNIType = "kindnet"
)

// CNIManager manages CNI installations
type CNIManager struct {
	config    *config.Config
	sshClient *ssh.Client
	// k8sClient is an optional Kubernetes client for direct API access
	k8sClient *k8s.Client
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
		sshClient: ssh.NewClient(&cfg.SSH),
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
		sshClient: ssh.NewClient(&cfg.SSH),
		k8sClient: k8sClient,
	}, nil
}

// K8sClient returns the underlying Kubernetes client for advanced operations
func (m *CNIManager) K8sClient() *k8s.Client {
	return m.k8sClient
}
