package kind

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wizhao/dpu-sim/pkg/cni"
	"github.com/wizhao/dpu-sim/pkg/k8s"
	"github.com/wizhao/dpu-sim/pkg/linux"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
	"go.yaml.in/yaml/v2"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"
)

func (m *KindManager) InstallDependencies(cmdExec platform.CommandExecutor) error {
	deps := []platform.Dependency{
		{
			Name:        "IPv6",
			Reason:      "Required for Kind clusters",
			CheckFunc:   linux.CheckIpv6,
			InstallFunc: linux.ConfigureIpv6,
		},
	}
	return platform.EnsureDependenciesWithExecutor(cmdExec, deps, m.config)
}

// DeployAllClusters deploys all Kind clusters defined in the config
func (m *KindManager) DeployAllClusters() error {
	for _, cluster := range m.config.Kubernetes.Clusters {
		// Build Kind config
		kindCfg, err := m.BuildKindConfig(cluster.Name, cluster)
		if err != nil {
			return fmt.Errorf("failed to build Kind config for %s: %w", cluster.Name, err)
		}

		if err := m.createCluster(cluster.Name, kindCfg); err != nil {
			return fmt.Errorf("failed to create cluster %s: %w", cluster.Name, err)
		}

		kubeconfigPath := k8s.GetKubeconfigPath(cluster.Name, m.config.Kubernetes.GetKubeconfigDir())
		if err := m.GetKubeconfig(cluster.Name, kubeconfigPath); err != nil {
			return fmt.Errorf("failed to save kubeconfig for %s: %w", cluster.Name, err)
		}
	}
	return nil
}

func (m *KindManager) InstallCNI() error {
	for _, cluster := range m.config.Kubernetes.Clusters {
		log.Info("\n--- Installing CNI on cluster %s ---", cluster.Name)
		cniType := cni.CNIType(cluster.CNI)

		if cniType == cni.CNIOVNKubernetes {
			if err := m.PullAndLoadImage(cluster.Name, cni.DefaultOVNImage); err != nil {
				return fmt.Errorf("failed to load OVN-Kubernetes image: %w", err)
			}
		}

		kubeconfigContent, err := m.GetKubeconfigContent(cluster.Name)
		if err != nil {
			return fmt.Errorf("failed to get kubeconfig for cluster %s: %w", cluster.Name, err)
		}

		cniMgr, err := cni.NewCNIManagerWithKubeconfig(m.config, kubeconfigContent)
		if err != nil {
			return fmt.Errorf("failed to create CNI manager: %w", err)
		}

		apiServerIP, err := m.GetInternalAPIServerIP(cluster.Name)
		if err != nil {
			return fmt.Errorf("failed to get internal API server IP for cluster %s: %w", cluster.Name, err)
		}

		if err := cniMgr.InstallCNI(cniType, cluster.Name, apiServerIP); err != nil {
			return fmt.Errorf("failed to install CNI on cluster %s: %w", cluster.Name, err)
		}
	}

	log.Info("\n✓ CNI installation complete on Kind clusters")
	return nil
}

// createCluster creates a new Kind cluster using the v1alpha4.Cluster config directly
func (m *KindManager) createCluster(name string, cfg *v1alpha4.Cluster) error {
	// Check if cluster already exists
	if m.ClusterExists(name) {
		log.Info("Kind cluster %s already exists, skipping creation", name)
		return nil
	}

	log.Info("Creating Kind cluster: %s", name)

	var opts []cluster.CreateOption
	if cfg != nil {
		data, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("failed to marshal Kind config: %w", err)
		}

		log.Debug("Generated Kind config:")
		log.Debug("%s", string(data))

		opts = append(opts, cluster.CreateWithV1Alpha4Config(cfg))
	}

	if err := m.provider.Create(name, opts...); err != nil {
		return fmt.Errorf("failed to create Kind cluster %s: %w", name, err)
	}

	log.Info("✓ Created Kind cluster: %s", name)
	return nil
}

// DeleteCluster deletes a Kind cluster
func (m *KindManager) DeleteCluster(name string) error {
	if !m.ClusterExists(name) {
		log.Info("Kind cluster %s does not exist, skipping deletion", name)
		return nil
	}

	log.Info("Deleting Kind cluster: %s", name)

	if err := m.provider.Delete(name, ""); err != nil {
		return fmt.Errorf("failed to delete Kind cluster %s: %w", name, err)
	}

	log.Info("✓ Deleted Kind cluster: %s", name)
	return nil
}

// ClusterExists checks if a Kind cluster exists
func (m *KindManager) ClusterExists(name string) bool {
	clusters, err := m.provider.List()
	if err != nil {
		return false
	}

	for _, cluster := range clusters {
		if cluster == name {
			return true
		}
	}
	return false
}

// ListClusters lists all Kind clusters
func (m *KindManager) ListClusters() ([]string, error) {
	clusters, err := m.provider.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list Kind clusters: %w", err)
	}
	return clusters, nil
}

// GetClusterInfo retrieves information about a Kind cluster
func (m *KindManager) GetClusterInfo(name string) (*ClusterInfo, error) {
	if !m.ClusterExists(name) {
		return nil, fmt.Errorf("cluster %s does not exist", name)
	}

	info := &ClusterInfo{
		Name:   name,
		Status: "running",
		Nodes:  []NodeInfo{},
	}

	// Get nodes using the kind provider
	nodes, err := m.provider.ListNodes(name)
	if err != nil {
		return info, nil // Return partial info on error
	}

	for _, node := range nodes {
		nodeName := node.String()
		role := "worker"
		if strings.Contains(nodeName, "control-plane") {
			role = "control-plane"
		}

		// Check node status using kubectl (still needed for detailed status)
		status := "Unknown"
		cmd := exec.Command("kubectl", "get", "node", nodeName,
			"--context", fmt.Sprintf("kind-%s", name),
			"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
		output, err := cmd.Output()
		if err == nil {
			if strings.TrimSpace(string(output)) == "True" {
				status = "Ready"
			} else {
				status = "NotReady"
			}
		}

		info.Nodes = append(info.Nodes, NodeInfo{
			Name:   nodeName,
			Role:   role,
			Status: status,
		})
	}

	return info, nil
}

// GetKubeconfig retrieves the kubeconfig for a Kind cluster
func (m *KindManager) GetKubeconfig(name string, kubeconfigPath string) error {
	if !m.ClusterExists(name) {
		return fmt.Errorf("cluster %s does not exist", name)
	}

	kubeconfig, err := m.provider.KubeConfig(name, false)
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig for cluster %s: %w", name, err)
	}

	// Ensure parent directory exists
	kubeconfigDir := filepath.Dir(kubeconfigPath)
	if err := os.MkdirAll(kubeconfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create kubeconfig directory %s: %w", kubeconfigDir, err)
	}

	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0600); err != nil {
		return fmt.Errorf("failed to write kubeconfig to %s: %w", kubeconfigPath, err)
	}

	log.Info("✓ Kubeconfig saved to: %s", kubeconfigPath)
	return nil
}

func (m *KindManager) GetKubeconfigContent(name string) (string, error) {
	if !m.ClusterExists(name) {
		return "", fmt.Errorf("cluster %s does not exist", name)
	}

	return m.provider.KubeConfig(name, false)
}

// LoadImage loads a Docker image into a Kind cluster
func (m *KindManager) LoadImage(clusterName, imageName string) error {
	if !m.ClusterExists(clusterName) {
		return fmt.Errorf("cluster %s does not exist", clusterName)
	}

	log.Info("Loading image %s into cluster %s...", imageName, clusterName)

	// Get the nodes for this cluster
	nodes, err := m.provider.ListNodes(clusterName)
	if err != nil {
		return fmt.Errorf("failed to list nodes for cluster %s: %w", clusterName, err)
	}

	// Convert nodes to node names for LoadImageArchive
	nodeNames := make([]string, len(nodes))
	for i, node := range nodes {
		nodeNames[i] = node.String()
	}

	// Use the provider's internal node loading - fall back to exec for now
	// as the library doesn't expose a simple LoadImage function
	cmd := exec.Command("kind", "load", "docker-image", imageName, "--name", clusterName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to load image %s: %w", imageName, err)
	}

	log.Info("✓ Loaded image: %s", imageName)
	return nil
}

// ExportLogs exports logs from a Kind cluster
func (m *KindManager) ExportLogs(clusterName, outputDir string) error {
	if !m.ClusterExists(clusterName) {
		return fmt.Errorf("cluster %s does not exist", clusterName)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	log.Info("Exporting logs from cluster %s to %s...", clusterName, outputDir)

	if err := m.provider.CollectLogs(clusterName, outputDir); err != nil {
		return fmt.Errorf("failed to export logs: %w", err)
	}

	log.Info("✓ Logs exported to: %s", outputDir)
	return nil
}

// GetInternalAPIServerIP retrieves the internal API server IP for a Kind cluster.
// This returns the control plane node's (or load balancer's) IP address
// in the kind network, suitable for in-cluster communication (e.g., https://172.18.0.2:6443).
// For HA clusters with multiple control planes, this returns the load balancer IP.
func (m *KindManager) GetInternalAPIServerIP(clusterName string) (string, error) {
	if !m.ClusterExists(clusterName) {
		return "", fmt.Errorf("cluster %s does not exist", clusterName)
	}

	// Get the internal kubeconfig which contains the control plane node's DNS name
	// (or load balancer DNS name for HA clusters)
	internalKubeconfig, err := m.provider.KubeConfig(clusterName, true) // true = internal
	if err != nil {
		return "", fmt.Errorf("failed to get internal kubeconfig: %w", err)
	}

	// Extract the server URL from the kubeconfig
	var serverURL string
	for _, line := range strings.Split(internalKubeconfig, "\n") {
		if strings.Contains(line, "server:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				serverURL = parts[1]
				break
			}
		}
	}

	if serverURL == "" {
		return "", fmt.Errorf("failed to extract server URL from kubeconfig")
	}

	// Extract the node name (e.g., https://cluster-control-plane:6443 -> cluster-control-plane)
	// This could be a control plane node or a load balancer for HA clusters
	hostPart := strings.TrimPrefix(serverURL, "https://")
	nodeName := strings.Split(hostPart, ":")[0]

	// Get the node IP address from Docker using the kind network
	cmd := exec.Command("docker", "inspect", "-f",
		"{{.NetworkSettings.Networks.kind.IPAddress}}", nodeName)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get node IP for %s: %w", nodeName, err)
	}

	nodeIP := strings.TrimSpace(string(output))
	if nodeIP == "" {
		return "", fmt.Errorf("node IP is empty for %s", nodeName)
	}

	log.Info("Internal API server IP for cluster %s: %s", clusterName, nodeIP)
	return nodeIP, nil
}

// PullAndLoadImage pulls a Docker image from a registry and loads it into a Kind cluster.
// If the image already exists locally, it skips the pull step.
func (m *KindManager) PullAndLoadImage(clusterName, imageName string) error {
	if !m.ClusterExists(clusterName) {
		return fmt.Errorf("cluster %s does not exist", clusterName)
	}

	log.Info("Pulling image %s...", imageName)

	// Pull the image first to ensure it exists locally
	pullCmd := exec.Command("docker", "pull", imageName)
	pullCmd.Stdout = os.Stdout
	pullCmd.Stderr = os.Stderr

	if err := pullCmd.Run(); err != nil {
		return fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}

	log.Info("✓ Pulled image: %s", imageName)

	// Load the image into Kind
	return m.LoadImage(clusterName, imageName)
}
