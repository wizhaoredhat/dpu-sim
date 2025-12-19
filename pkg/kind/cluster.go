package kind

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CreateCluster creates a new Kind cluster
func (m *Manager) CreateCluster(name string, configPath string) error {
	// Check if cluster already exists
	if m.ClusterExists(name) {
		fmt.Printf("Kind cluster %s already exists, skipping creation\n", name)
		return nil
	}

	fmt.Printf("Creating Kind cluster: %s\n", name)

	args := []string{"create", "cluster", "--name", name}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}

	cmd := exec.Command("kind", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create Kind cluster %s: %w", name, err)
	}

	fmt.Printf("✓ Created Kind cluster: %s\n", name)
	return nil
}

// DeleteCluster deletes a Kind cluster
func (m *Manager) DeleteCluster(name string) error {
	if !m.ClusterExists(name) {
		fmt.Printf("Kind cluster %s does not exist, skipping deletion\n", name)
		return nil
	}

	fmt.Printf("Deleting Kind cluster: %s\n", name)

	cmd := exec.Command("kind", "delete", "cluster", "--name", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete Kind cluster %s: %w", name, err)
	}

	fmt.Printf("✓ Deleted Kind cluster: %s\n", name)
	return nil
}

// ClusterExists checks if a Kind cluster exists
func (m *Manager) ClusterExists(name string) bool {
	cmd := exec.Command("kind", "get", "clusters")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	clusters := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, cluster := range clusters {
		if strings.TrimSpace(cluster) == name {
			return true
		}
	}
	return false
}

// ListClusters lists all Kind clusters
func (m *Manager) ListClusters() ([]string, error) {
	cmd := exec.Command("kind", "get", "clusters")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list Kind clusters: %w", err)
	}

	if len(output) == 0 {
		return []string{}, nil
	}

	clusters := strings.Split(strings.TrimSpace(string(output)), "\n")
	result := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		if trimmed := strings.TrimSpace(cluster); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result, nil
}

// GetClusterInfo retrieves information about a Kind cluster
func (m *Manager) GetClusterInfo(name string) (*ClusterInfo, error) {
	if !m.ClusterExists(name) {
		return nil, fmt.Errorf("cluster %s does not exist", name)
	}

	info := &ClusterInfo{
		Name:   name,
		Status: "running",
		Nodes:  []NodeInfo{},
	}

	// Get nodes using kubectl
	cmd := exec.Command("kubectl", "get", "nodes", 
		"--context", fmt.Sprintf("kind-%s", name),
		"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\t\"}{.status.conditions[?(@.type=='Ready')].status}{\"\\n\"}{end}")
	
	output, err := cmd.Output()
	if err != nil {
		return info, nil // Return partial info on error
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 2 {
			nodeName := parts[0]
			status := "NotReady"
			if parts[1] == "True" {
				status = "Ready"
			}
			
			role := "worker"
			if strings.Contains(nodeName, "control-plane") {
				role = "control-plane"
			}
			
			info.Nodes = append(info.Nodes, NodeInfo{
				Name:   nodeName,
				Role:   role,
				Status: status,
			})
		}
	}

	return info, nil
}

// GetKubeconfig retrieves the kubeconfig for a Kind cluster
func (m *Manager) GetKubeconfig(name string, outputPath string) error {
	if !m.ClusterExists(name) {
		return fmt.Errorf("cluster %s does not exist", name)
	}

	// Create output directory if it doesn't exist
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	cmd := exec.Command("kind", "get", "kubeconfig", "--name", name)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig for cluster %s: %w", name, err)
	}

	if err := os.WriteFile(outputPath, output, 0600); err != nil {
		return fmt.Errorf("failed to write kubeconfig to %s: %w", outputPath, err)
	}

	fmt.Printf("✓ Kubeconfig saved to: %s\n", outputPath)
	return nil
}

// LoadImage loads a Docker image into a Kind cluster
func (m *Manager) LoadImage(clusterName, imageName string) error {
	if !m.ClusterExists(clusterName) {
		return fmt.Errorf("cluster %s does not exist", clusterName)
	}

	fmt.Printf("Loading image %s into cluster %s...\n", imageName, clusterName)

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
func (m *Manager) ExportLogs(clusterName, outputDir string) error {
	if !m.ClusterExists(clusterName) {
		return fmt.Errorf("cluster %s does not exist", clusterName)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	fmt.Printf("Exporting logs from cluster %s to %s...\n", clusterName, outputDir)

	cmd := exec.Command("kind", "export", "logs", outputDir, "--name", clusterName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to export logs: %w", err)
	}

	fmt.Printf("✓ Logs exported to: %s\n", outputDir)
	return nil
}
