package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/kind"
	"github.com/wizhao/dpu-sim/pkg/linux"
)

var (
	configPath  string
	noCleanup   bool
	cleanupOnly bool
)

var rootCmd = &cobra.Command{
	Use:   "deploy-kind",
	Short: "Deploy Kind clusters",
	Long:  `Deploy Kubernetes clusters using Kind (Kubernetes in Docker) for DPU simulation`,
	RunE:  runKindDeploy,
}

func init() {
	rootCmd.Flags().StringVar(&configPath, "config", "config.yaml", "Path to configuration file")
	rootCmd.Flags().BoolVar(&noCleanup, "no-cleanup", false, "Skip cleanup of existing clusters before deployment")
	rootCmd.Flags().BoolVar(&cleanupOnly, "cleanup-only", false, "Only cleanup existing clusters, do not deploy")
}

func runKindDeploy(cmd *cobra.Command, args []string) error {
	fmt.Println("=== Kind Cluster Deployment ===")
	fmt.Printf("Configuration: %s\n", configPath)

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Ensure dpu-sim dependencies are installed
	if err := linux.EnsureDependencies(cfg); err != nil {
		return fmt.Errorf("dependency check failed: %w", err)
	}

	// Validate prerequisites
	fmt.Println("\n=== Validating Prerequisites ===")
	if err := kind.ValidateKindInstallation(); err != nil {
		return fmt.Errorf("prerequisite check failed: %w", err)
	}
	if err := kind.ValidateDockerInstallation(); err != nil {
		return fmt.Errorf("prerequisite check failed: %w", err)
	}

	// Create Kind manager
	kindMgr := kind.NewManager(cfg)

	// Cleanup if requested
	if !noCleanup || cleanupOnly {
		fmt.Println("\n=== Cleaning up existing clusters ===")
		for _, cluster := range cfg.Kubernetes.Clusters {
			if err := kindMgr.DeleteCluster(cluster.Name); err != nil {
				fmt.Printf("Warning: failed to delete cluster %s: %v\n", cluster.Name, err)
			}
		}
		if cleanupOnly {
			fmt.Println("✓ Cleanup complete")
			return nil
		}
	}

	// Create clusters
	fmt.Println("\n=== Creating Kind Clusters ===")
	for _, cluster := range cfg.Kubernetes.Clusters {
		// Generate Kind config
		configDir := filepath.Join(".", "kind-configs")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}

		kindConfigPath := filepath.Join(configDir, fmt.Sprintf("%s-config.yaml", cluster.Name))
		if err := kindMgr.GenerateKindConfigForDPU(cluster.Name, cluster, kindConfigPath); err != nil {
			return fmt.Errorf("failed to generate Kind config for %s: %w", cluster.Name, err)
		}

		// Create cluster
		if err := kindMgr.CreateCluster(cluster.Name, kindConfigPath); err != nil {
			return fmt.Errorf("failed to create cluster %s: %w", cluster.Name, err)
		}

		// Save kubeconfig
		kubeconfigDir := filepath.Join(".", "kubeconfig")
		kubeconfigPath := filepath.Join(kubeconfigDir, fmt.Sprintf("%s.yaml", cluster.Name))
		if err := kindMgr.GetKubeconfig(cluster.Name, kubeconfigPath); err != nil {
			return fmt.Errorf("failed to save kubeconfig for %s: %w", cluster.Name, err)
		}
	}

	// Display cluster information
	fmt.Println("\n=== Cluster Information ===")
	for _, cluster := range cfg.Kubernetes.Clusters {
		info, err := kindMgr.GetClusterInfo(cluster.Name)
		if err != nil {
			fmt.Printf("Warning: failed to get info for %s: %v\n", cluster.Name, err)
			continue
		}

		fmt.Printf("\nCluster: %s\n", info.Name)
		fmt.Printf("  Status: %s\n", info.Status)
		fmt.Printf("  Nodes:\n")
		for _, node := range info.Nodes {
			fmt.Printf("    - %s (%s) [%s]\n", node.Name, node.Role, node.Status)
		}
	}

	fmt.Println("\n=== Deployment Complete ===")
	fmt.Println("Kind clusters are ready!")
	fmt.Println("\nNext steps:")
	fmt.Println("  - Kubeconfig files are in ./kubeconfig/")
	fmt.Println("  - Use 'kubectl --kubeconfig kubeconfig/<cluster>.yaml get nodes' to verify")
	fmt.Println("  - Run 'install-software' to install CNI components")

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
