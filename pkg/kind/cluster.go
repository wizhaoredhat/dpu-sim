package kind

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/wizhao/dpu-sim/pkg/k8s"
	"go.yaml.in/yaml/v2"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"
)

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

// createCluster creates a new Kind cluster using the v1alpha4.Cluster config directly
func (m *KindManager) createCluster(name string, cfg *v1alpha4.Cluster) error {
	// Check if cluster already exists
	if m.ClusterExists(name) {
		fmt.Printf("Kind cluster %s already exists, skipping creation\n", name)
		return nil
	}

	fmt.Printf("Creating Kind cluster: %s\n", name)

	var opts []cluster.CreateOption
	if cfg != nil {
		data, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("failed to marshal Kind config: %w", err)
		}

		fmt.Println("Generated Kind config:")
		fmt.Println(string(data))

		opts = append(opts, cluster.CreateWithV1Alpha4Config(cfg))
	}

	if err := m.provider.Create(name, opts...); err != nil {
		return fmt.Errorf("failed to create Kind cluster %s: %w", name, err)
	}

	fmt.Printf("✓ Created Kind cluster: %s\n", name)
	return nil
}

// DeleteCluster deletes a Kind cluster
func (m *KindManager) DeleteCluster(name string) error {
	if !m.ClusterExists(name) {
		fmt.Printf("Kind cluster %s does not exist, skipping deletion\n", name)
		return nil
	}

	fmt.Printf("Deleting Kind cluster: %s\n", name)

	if err := m.provider.Delete(name, ""); err != nil {
		return fmt.Errorf("failed to delete Kind cluster %s: %w", name, err)
	}

	fmt.Printf("✓ Deleted Kind cluster: %s\n", name)
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

	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0600); err != nil {
		return fmt.Errorf("failed to write kubeconfig to %s: %w", kubeconfigPath, err)
	}

	fmt.Printf("✓ Kubeconfig saved to: %s\n", kubeconfigPath)
	return nil
}

// LoadImage loads a Docker image into a Kind cluster
func (m *KindManager) LoadImage(clusterName, imageName string) error {
	if !m.ClusterExists(clusterName) {
		return fmt.Errorf("cluster %s does not exist", clusterName)
	}

	fmt.Printf("Loading image %s into cluster %s...\n", imageName, clusterName)

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

	fmt.Printf("✓ Loaded image: %s\n", imageName)
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

	fmt.Printf("Exporting logs from cluster %s to %s...\n", clusterName, outputDir)

	if err := m.provider.CollectLogs(clusterName, outputDir); err != nil {
		return fmt.Errorf("failed to export logs: %w", err)
	}

	fmt.Printf("✓ Logs exported to: %s\n", outputDir)
	return nil
}
