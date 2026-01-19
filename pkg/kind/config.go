package kind

import (
	"fmt"
	"os/exec"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"github.com/wizhao/dpu-sim/pkg/config"
)

// BuildKindConfig builds a Kind cluster configuration using the kind library's data structures
func (m *KindManager) BuildKindConfig(clusterName string, clusterCfg config.ClusterConfig) (*v1alpha4.Cluster, error) {
	cluster := &v1alpha4.Cluster{
		TypeMeta: v1alpha4.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "kind.x-k8s.io/v1alpha4",
		},
		Name: clusterName,
	}

	// Networking configuration
	cluster.Networking = v1alpha4.Networking{
		PodSubnet:     clusterCfg.PodCIDR,
		ServiceSubnet: clusterCfg.ServiceCIDR,
		IPFamily:      v1alpha4.IPv4Family,
	}

	// Disable default CNI for custom CNI installation
	if clusterCfg.CNI != "" && clusterCfg.CNI != "kindnet" {
		cluster.Networking.DisableDefaultCNI = true
		cluster.Networking.KubeProxyMode = v1alpha4.ProxyMode("none")
	}

	// Nodes configuration for kind configuration
	if m.config.Kind != nil && len(m.config.Kind.Nodes) > 0 {
		for i, node := range m.config.Kind.Nodes {
			kindNode := v1alpha4.Node{
				Role: v1alpha4.NodeRole(node.Role),
			}

			// Add extra port mappings for the first control-plane node
			if node.Role == "control-plane" && i == 0 {
				kindNode.KubeadmConfigPatches = []string{
					`kind: InitConfiguration
nodeRegistration:
  kubeletExtraArgs:
    node-labels: "ingress-ready=true"
    authorization-mode: "AlwaysAllow"`,
				}
			}

			cluster.Nodes = append(cluster.Nodes, kindNode)
		}
	} else {
		return nil, fmt.Errorf("nodes configuration is required for kind configuration")
	}

	cluster.KubeadmConfigPatches = []string{
		`kind: ClusterConfiguration
metadata:
  name: config
apiServer:
  extraArgs:
    "v": "4"
controllerManager:
  extraArgs:
    "v": "4"
    "controllers": "*,bootstrap-signer-controller,token-cleaner-controller,-service-lb-controller"
scheduler:
  extraArgs:
    "v": "4"
networking:
  dnsDomain: "cluster.local"`,
		`kind: InitConfiguration
nodeRegistration:
  kubeletExtraArgs:
    "v": "4"`,
		`kind: JoinConfiguration
nodeRegistration:
  kubeletExtraArgs:
    "v": "4"`,
	}

	return cluster, nil
}

// ValidateDockerInstallation checks if Docker is installed and running
// This is required for the kind library to work
func ValidateDockerInstallation() error {
	cmd := exec.Command("docker", "ps")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker is not installed or not running: %w", err)
	}

	fmt.Println("✓ Docker is running")
	return nil
}
