package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/wizhao/dpu-sim/pkg/cni"
	"github.com/wizhao/dpu-sim/pkg/config"
)

var (
	configPath  string
	parallel    bool
	vmName      string
	skipCNI     bool
	clusterName string
)

var rootCmd = &cobra.Command{
	Use:   "install-software",
	Short: "Install software components on VMs or Kind clusters",
	Long:  `Install Kubernetes and CNI components on VMs via SSH or on Kind clusters via kubectl`,
	RunE:  runInstallSoftware,
}

func init() {
	rootCmd.Flags().StringVar(&configPath, "config", "config.yaml", "Path to configuration file")
	rootCmd.Flags().BoolVarP(&parallel, "parallel", "p", false, "Install on all VMs in parallel")
	rootCmd.Flags().StringVar(&vmName, "vm", "", "Install only on specific VM")
	rootCmd.Flags().BoolVar(&skipCNI, "skip-cni", false, "Skip CNI installation")
	rootCmd.Flags().StringVar(&clusterName, "cluster", "", "Target specific cluster (for Kind deployments)")
}

func runInstallSoftware(cmd *cobra.Command, args []string) error {
	fmt.Println("=== Software Installation ===")
	fmt.Printf("Configuration: %s\n", configPath)

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Determine deployment mode
	mode, err := cfg.GetDeploymentMode()
	if err != nil {
		return fmt.Errorf("failed to determine deployment mode: %w", err)
	}
	fmt.Printf("Deployment mode: %s\n", mode)

	switch mode {
	case "vm":
		return nil
	case "kind":
		return installOnKind(cfg)
	default:
		return fmt.Errorf("unknown deployment mode: %s", mode)
	}
}

func installOnKind(cfg *config.Config) error {
	fmt.Println("\n=== Installing CNI on Kind clusters ===")

	for _, cluster := range cfg.Kubernetes.Clusters {
		if clusterName != "" && cluster.Name != clusterName {
			continue
		}

		if skipCNI {
			fmt.Printf("Skipping CNI installation on cluster %s\n", cluster.Name)
			continue
		}

		fmt.Printf("\n--- Installing CNI on cluster %s ---\n", cluster.Name)

		kubeconfigPath := filepath.Join("kubeconfig", fmt.Sprintf("%s.yaml", cluster.Name))
		cniType := cni.CNIType(cluster.CNI)
		cniMgr, err := cni.NewCNIManagerWithKubeconfigFile(cfg, kubeconfigPath)
		if err != nil {
			return fmt.Errorf("failed to create CNI manager: %w", err)
		}

		if err := cniMgr.InstallCNI(cniType, cluster.Name); err != nil {
			return fmt.Errorf("failed to install CNI on cluster %s: %w", cluster.Name, err)
		}
	}

	fmt.Println("\n✓ CNI installation complete on Kind clusters")
	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
