package kind

import (
	"fmt"
	"os"
	"strings"
	"os/exec"

	"github.com/wizhao/dpu-sim/pkg/config"
)

// GenerateKindConfig generates a Kind cluster configuration file
func (m *Manager) GenerateKindConfig(clusterCfg config.ClusterConfig, outputPath string) error {
	var sb strings.Builder

	sb.WriteString("kind: Cluster\n")
	sb.WriteString("apiVersion: kind.x-k8s.io/v1alpha4\n")

	// Networking configuration
	sb.WriteString("networking:\n")
	if clusterCfg.PodCIDR != "" {
		sb.WriteString(fmt.Sprintf("  podSubnet: \"%s\"\n", clusterCfg.PodCIDR))
	}
	if clusterCfg.ServiceCIDR != "" {
		sb.WriteString(fmt.Sprintf("  serviceSubnet: \"%s\"\n", clusterCfg.ServiceCIDR))
	}
	
	// Disable default CNI if a custom CNI is specified
	if clusterCfg.CNI != "" && clusterCfg.CNI != "kindnet" {
		sb.WriteString("  disableDefaultCNI: true\n")
	}

	// Nodes configuration
	if m.config.Kind != nil && len(m.config.Kind.Nodes) > 0 {
		sb.WriteString("nodes:\n")
		for _, node := range m.config.Kind.Nodes {
			sb.WriteString(fmt.Sprintf("- role: %s\n", node.Role))
		}
	} else {
		// Default configuration: 1 control-plane node
		sb.WriteString("nodes:\n")
		sb.WriteString("- role: control-plane\n")
	}

	// Write to file
	if err := os.WriteFile(outputPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to write Kind config to %s: %w", outputPath, err)
	}

	fmt.Printf("✓ Generated Kind config: %s\n", outputPath)
	return nil
}

// GenerateKindConfigForDPU generates a Kind cluster configuration optimized for DPU simulation
func (m *Manager) GenerateKindConfigForDPU(clusterName string, clusterCfg config.ClusterConfig, outputPath string) error {
	var sb strings.Builder

	sb.WriteString("kind: Cluster\n")
	sb.WriteString("apiVersion: kind.x-k8s.io/v1alpha4\n")
	sb.WriteString(fmt.Sprintf("name: %s\n", clusterName))

	// Networking configuration
	sb.WriteString("networking:\n")
	if clusterCfg.PodCIDR != "" {
		sb.WriteString(fmt.Sprintf("  podSubnet: \"%s\"\n", clusterCfg.PodCIDR))
	}
	if clusterCfg.ServiceCIDR != "" {
		sb.WriteString(fmt.Sprintf("  serviceSubnet: \"%s\"\n", clusterCfg.ServiceCIDR))
	}
	
	// Disable default CNI for custom CNI installation
	if clusterCfg.CNI != "" && clusterCfg.CNI != "kindnet" {
		sb.WriteString("  disableDefaultCNI: true\n")
	}

	// Nodes configuration from Kind config
	if m.config.Kind != nil && len(m.config.Kind.Nodes) > 0 {
		sb.WriteString("nodes:\n")
		for i, node := range m.config.Kind.Nodes {
			sb.WriteString(fmt.Sprintf("- role: %s\n", node.Role))
			
			// Add extra port mappings for control-plane nodes
			if node.Role == "control-plane" && i == 0 {
				sb.WriteString("  extraPortMappings:\n")
				sb.WriteString("  - containerPort: 6443\n")
				sb.WriteString("    hostPort: 6443\n")
				sb.WriteString("    protocol: TCP\n")
			}

			// Mount host paths for DPU simulation if needed
			sb.WriteString("  extraMounts:\n")
			sb.WriteString("  - hostPath: /var/run/docker.sock\n")
			sb.WriteString("    containerPath: /var/run/docker.sock\n")
		}
	} else {
		// Default: single control-plane node
		sb.WriteString("nodes:\n")
		sb.WriteString("- role: control-plane\n")
		sb.WriteString("  extraPortMappings:\n")
		sb.WriteString("  - containerPort: 6443\n")
		sb.WriteString("    hostPort: 6443\n")
		sb.WriteString("    protocol: TCP\n")
	}

	// Write to file
	if err := os.WriteFile(outputPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to write Kind config to %s: %w", outputPath, err)
	}

	fmt.Printf("✓ Generated Kind config for DPU: %s\n", outputPath)
	return nil
}

// ValidateKindInstallation checks if Kind is installed and available
func ValidateKindInstallation() error {
	cmd := exec.Command("kind", "version")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("kind is not installed or not in PATH: %w", err)
	}

	fmt.Printf("Kind version: %s\n", strings.TrimSpace(string(output)))
	return nil
}

// ValidateDockerInstallation checks if Docker is installed and running
func ValidateDockerInstallation() error {
	cmd := exec.Command("docker", "ps")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker is not installed or not running: %w", err)
	}

	fmt.Println("✓ Docker is running")
	return nil
}
