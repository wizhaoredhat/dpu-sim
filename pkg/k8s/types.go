package k8s

import (
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/platform"
	"github.com/wizhao/dpu-sim/pkg/ssh"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
)

// Client wraps Kubernetes client-go for cluster operations
type Client struct {
	clientset       *kubernetes.Clientset
	dynamicClient   dynamic.Interface
	cachedDiscovery discovery.CachedDiscoveryInterface
	restMapper      *restmapper.DeferredDiscoveryRESTMapper
}

// K8sMachineManager manages Kubernetes cluster operations
type K8sMachineManager struct {
	config      *config.Config
	sshClient   *ssh.Client
	linuxDistro *platform.Distro
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

// ControlPlaneInfo contains the information needed to join nodes to a cluster
// and connect to the Kubernetes API server
type ControlPlaneInfo struct {
	// WorkerJoinCommand is the full join command for worker nodes
	WorkerJoinCommand string
	// ControlPlaneJoinCommand is the full join command for additional control plane nodes
	ControlPlaneJoinCommand string
	// APIServerEndpoint is the URL to connect to the Kubernetes API server (e.g., https://192.168.1.10:6443)
	APIServerEndpoint string
	// Kubeconfig is the admin kubeconfig content for connecting to the cluster
	Kubeconfig string
}

// NewK8sMachineManager creates a new Kubernetes manager
func NewK8sMachineManager(cfg *config.Config) *K8sMachineManager {
	return &K8sMachineManager{
		config:      cfg,
		sshClient:   ssh.NewClient(&cfg.SSH),
		linuxDistro: nil,
	}
}
