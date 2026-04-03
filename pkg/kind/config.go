package kind

import (
	"fmt"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"github.com/wizhao/dpu-sim/pkg/config"
)

// Node label used to identify the config "name" since Kind does not support node renaming.
const kindNodeNameLabel = "dpu-sim.org/node-name"

// BuildKindConfig builds a Kind cluster configuration using the kind library's data structures.
// Only nodes with k8s_cluster == clusterName are included; each node gets a label with its config name.
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

	// Disable default CNI for custom CNI installation.
	// Keep kube-proxy enabled for CNIs like flannel that rely on Service VIP routing.
	if clusterCfg.CNI != "" && clusterCfg.CNI != "kindnet" {
		cluster.Networking.DisableDefaultCNI = true
	}

	// OVN-Kubernetes in this project programs service handling itself, so disable
	// kube-proxy only for OVN Kind clusters.
	if clusterCfg.CNI == config.CNIOVNKubernetes {
		cluster.Networking.KubeProxyMode = v1alpha4.ProxyMode("none")
	}

	// Nodes: only those belonging to this cluster, with role from k8s_role and name label
	nodes := m.config.GetKindNodesForCluster(clusterName)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no kind nodes for cluster %s", clusterName)
	}

	firstControlPlane := true
	for _, node := range nodes {
		role := node.K8sRole
		if role == "master" {
			role = "control-plane"
		}
		kindNode := v1alpha4.Node{
			Role: v1alpha4.NodeRole(role),
		}
		// Label to identify this node by config name (Kind does not support renaming)
		label := fmt.Sprintf("%s=%s", kindNodeNameLabel, node.Name)
		labelsArg := label
		if role == "control-plane" && firstControlPlane {
			labelsArg = label + ",ingress-ready=true"
			firstControlPlane = false
			kindNode.KubeadmConfigPatches = []string{
				fmt.Sprintf(`kind: InitConfiguration
nodeRegistration:
  kubeletExtraArgs:
    node-labels: "%s"
    authorization-mode: "AlwaysAllow"`, labelsArg),
			}
		} else {
			kindNode.KubeadmConfigPatches = []string{
				fmt.Sprintf(`kind: InitConfiguration
nodeRegistration:
  kubeletExtraArgs:
    node-labels: "%s"`, labelsArg),
			}
		}
		cluster.Nodes = append(cluster.Nodes, kindNode)
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

	// If a local registry is configured, tell containerd to look up
	// per-host registry configuration from /etc/containerd/certs.d/.
	// The actual host config files are written after cluster creation
	// via ConfigureRegistryOnNodes.
	if m.config.IsRegistryEnabled() {
		cluster.ContainerdConfigPatches = append(cluster.ContainerdConfigPatches,
			`[plugins."io.containerd.cri.v1.images".registry]
  config_path = "/etc/containerd/certs.d"`)
	}

	return cluster, nil
}
