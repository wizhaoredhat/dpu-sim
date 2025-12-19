package k8s

import (
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/ssh"
)

// K8sMachineManager manages Kubernetes cluster operations
type K8sMachineManager struct {
	config    *config.Config
	sshClient *ssh.Client
}

// ClusterStatus represents the status of a Kubernetes cluster
type ClusterStatus struct {
	Name         string
	IsReady      bool
	Nodes        []NodeStatus
	KubeVersion  string
	ControlPlane []string
	Workers      []string
}

// NodeStatus represents the status of a Kubernetes node
type NodeStatus struct {
	Name   string
	Status string
	Role   string
	IP     string
}

// NewK8sMachineManager creates a new Kubernetes manager
func NewK8sMachineManager(cfg *config.Config) *K8sMachineManager {
	return &K8sMachineManager{
		config:    cfg,
		sshClient: ssh.NewClient(&cfg.SSH),
	}
}
