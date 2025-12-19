package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/wizhao/dpu-sim/pkg/cni"
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/kind"
	"github.com/wizhao/dpu-sim/pkg/platform"
	"github.com/wizhao/dpu-sim/pkg/ssh"
	"github.com/wizhao/dpu-sim/pkg/vm"
	"libvirt.org/go/libvirt"
)

var (
	// Global flags
	configPath string

	// Root command flags
	skipDeps    bool
	skipCleanup bool
	cleanupOnly bool
	skipDeploy  bool
	deployOnly  bool
	skipK8s     bool
	k8sOnly     bool

	// deploy-kind flags
	kindNoCleanup   bool
	kindCleanupOnly bool

	// install-software flags
	installParallel bool
	installVMName   string
	installSkipCNI  bool
	installCluster  string
)

var rootCmd = &cobra.Command{
	Use:   "dpu-sim",
	Short: "DPU Simulator - Complete setup orchestration",
	Long: `DPU Simulator automates deployment of DPU simulation environments
using either VMs (libvirt) or containers (Kind), pre-configured with
Kubernetes and CNI for container networking experiments.

This is the main orchestrator that runs the complete deployment workflow:
  1. Deploy infrastructure (VMs or Kind clusters)
  2. Install Kubernetes and CNI components
  3. Verify deployment

For more control, use subcommands:
  - vmctl: Manage VMs (separate command)`,
	RunE: runDeploy,
}

var installSoftwareCmd = &cobra.Command{
	Use:   "install-software",
	Short: "Install software components on VMs or Kind clusters",
	Long:  `Install Kubernetes and CNI components on VMs via SSH or on Kind clusters via kubectl`,
	RunE:  runInstallSoftwareCmd,
}

func init() {
	// Global persistent flag
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "config.yaml", "Path to configuration file")

	// Root command flags
	rootCmd.Flags().BoolVar(&cleanupOnly, "cleanup", false, "Only cleanup existing resources, do not deploy")
	rootCmd.Flags().BoolVar(&skipDeps, "skip-deps", false, "Skip dependency checks")
	rootCmd.Flags().BoolVar(&skipCleanup, "skip-cleanup", false, "Skip cleanup of existing resources")
	rootCmd.Flags().BoolVar(&skipDeploy, "skip-deploy", false, "Skip VM/Kind deployment")
	rootCmd.Flags().BoolVar(&skipK8s, "skip-k8s", false, "Skip Kubernetes and CNI installation")

	// install-software flags
	installSoftwareCmd.Flags().BoolVarP(&installParallel, "parallel", "p", false, "Install on all VMs in parallel")
	installSoftwareCmd.Flags().StringVar(&installVMName, "vm", "", "Install only on specific VM")
	installSoftwareCmd.Flags().BoolVar(&installSkipCNI, "skip-cni", false, "Skip CNI installation")
	installSoftwareCmd.Flags().StringVar(&installCluster, "cluster", "", "Target specific cluster (for Kind deployments)")

	// Add subcommands
	rootCmd.AddCommand(installSoftwareCmd)
}

// =============================================================================
// Root command - Orchestrated deployment
// =============================================================================

func runDeploy(cmd *cobra.Command, args []string) error {
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║               DPU Simulator                   ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Printf("\nConfiguration: %s\n", configPath)

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if !skipDeps {
		fmt.Println("\n=== Checking Dependencies ===")
		if err := platform.EnsureDependencies(cfg); err != nil {
			return fmt.Errorf("dependency check failed: %w", err)
		}
	} else {
		fmt.Println("\nSkipping dependency check")
	}

	deployMode, err := cfg.GetDeploymentMode()
	if err != nil {
		return fmt.Errorf("failed to determine deployment mode: %w", err)
	}
	fmt.Printf("Deployment mode: %s\n", deployMode)

	switch deployMode {
	case "vm":
		return runVMDeploymentWorkflow(cfg)
	case "kind":
		return runKindDeploymentWorkflow(cfg)
	default:
		return fmt.Errorf("unknown deployment mode: %s", deployMode)
	}
}

func runVMDeploymentWorkflow(cfg *config.Config) error {
	fmt.Println("")
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║       VM-Based Deployment Workflow            ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")

	conn, err := vm.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to libvirt: %w", err)
	}
	defer conn.Close()

	if !skipCleanup || cleanupOnly {
		fmt.Println("\n=== Cleaning up VMs and networks ===")
		if err := vm.CleanupAll(cfg, conn); err != nil {
			fmt.Printf("Warning: cleanup failed: %v\n", err)
		}
		if cleanupOnly {
			fmt.Println("\n✓ Cleanup complete. No deployment performed.")
			return nil
		}
	}

	if !skipDeploy {
		fmt.Println("\n=== Deploying VMs ===")
		if err := doVMDeploy(cfg, conn); err != nil {
			return fmt.Errorf("VM deployment failed: %w", err)
		}
	} else {
		fmt.Println("\nSkipping VM deployment")
	}

	if !skipK8s {
		fmt.Println("\n=== Installing Kubernetes and CNI ===")
		if err := doVMInstallK8s(cfg, conn); err != nil {
			return fmt.Errorf("Kubernetes installation failed: %w", err)
		}
	} else {
		fmt.Println("\nSkipping Kubernetes installation")
	}

	printSuccessMessage("VM")
	return nil
}

func runKindDeploymentWorkflow(cfg *config.Config) error {
	fmt.Println("")
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║      Kind-Based Deployment Workflow           ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")

	// Step 1: Deploy Kind clusters
	if !skipDeploy {
		fmt.Println("\n[Step 1/2] Deploying Kind clusters...")
		if err := doKindDeploy(cfg, !skipCleanup, false); err != nil {
			return fmt.Errorf("Kind deployment failed: %w", err)
		}
	} else {
		fmt.Println("\n[Step 1/2] Skipping Kind deployment")
	}

	// Step 2: Install CNI
	if !skipK8s {
		fmt.Println("\n[Step 2/2] Installing CNI...")
		if err := doInstallSoftware(cfg, "", false, ""); err != nil {
			return fmt.Errorf("CNI installation failed: %w", err)
		}
	} else {
		fmt.Println("\n[Step 2/2] Skipping CNI installation")
	}

	printSuccessMessage("Kind")
	return nil
}

func printSuccessMessage(deployType string) {
	fmt.Println("")
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║         Deployment Completed Successfully!    ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")

	if deployType == "VM" {
		fmt.Println("\n✓ VM deployment complete!")
		fmt.Println("\nYour DPU simulation environment is ready:")
		fmt.Println("  • VMs are running and accessible")
		fmt.Println("  • Kubernetes is installed and configured")
		fmt.Println("  • CNI is deployed and ready")
		fmt.Println("\nUseful commands:")
		fmt.Println("  vmctl list                    # List all VMs")
		fmt.Println("  vmctl ssh <vm-name>           # SSH into a VM")
		fmt.Println("  kubectl --kubeconfig kubeconfig/<cluster>.kubeconfig get nodes")
	} else {
		fmt.Println("\n✓ Kind deployment complete!")
		fmt.Println("\nYour DPU simulation environment is ready:")
		fmt.Println("  • Kind clusters are running")
		fmt.Println("  • CNI is deployed and ready")
		fmt.Println("\nUseful commands:")
		fmt.Println("  kind get clusters             # List all clusters")
		fmt.Println("  kubectl --kubeconfig kubeconfig/<cluster>.kubeconfig get nodes")
		fmt.Println("  kubectl --kubeconfig kubeconfig/<cluster>.kubeconfig get pods -A")
	}

	fmt.Println("\nKubeconfig files: ./kubeconfig/")
	fmt.Println("\nFor more information, see README.md")
}

func doVMDeploy(cfg *config.Config, conn *libvirt.Connect) error {
	// Create networks
	if err := vm.CreateAllNetworks(cfg, conn); err != nil {
		return fmt.Errorf("failed to create networks: %w", err)
	}

	// Create VMs
	if err := vm.CreateAllVMs(cfg, conn); err != nil {
		return fmt.Errorf("failed to create VMs: %w", err)
	}

	// Wait for VMs to get IP addresses
	fmt.Println("\n=== Waiting for VMs to boot and get IPs ===")
	sshClient := ssh.NewClient(&cfg.SSH)

	for _, vmCfg := range cfg.VMs {
		fmt.Printf("Waiting for %s to get an IP address...\n", vmCfg.Name)
		ip, err := vm.WaitForVMIP(conn, vmCfg.Name, config.MgmtNetworkName, cfg, 5*time.Minute)
		if err != nil {
			return fmt.Errorf("failed to get IP for %s: %w", vmCfg.Name, err)
		}
		fmt.Printf("✓ %s IP: %s\n", vmCfg.Name, ip)

		// Wait for SSH to be ready
		fmt.Printf("Waiting for SSH on %s...\n", vmCfg.Name)
		if err := sshClient.WaitForSSH(ip, 5*time.Minute); err != nil {
			return fmt.Errorf("failed to connect to SSH on %s: %w", vmCfg.Name, err)
		}
		fmt.Printf("✓ SSH ready on %s\n", vmCfg.Name)
	}

	return nil
}

func doVMInstallK8s(cfg *config.Config, conn *libvirt.Connect) error {
	if err := vm.InstallKubernetes(conn, cfg, ""); err != nil {
		return fmt.Errorf("failed to install Kubernetes: %w", err)
	}

	if err := vm.SetupAllK8sClusters(conn, cfg); err != nil {
		return fmt.Errorf("failed to setup Kubernetes clusters: %w", err)
	}

	return nil
}

func doKindDeploy(cfg *config.Config, cleanup bool, cleanupOnly bool) error {
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
	if cleanup || cleanupOnly {
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
	fmt.Println("  - Run 'dpu-sim install-software' to install CNI components")

	return nil
}

func runInstallSoftwareCmd(cmd *cobra.Command, args []string) error {
	fmt.Println("=== Software Installation ===")
	fmt.Printf("Configuration: %s\n", configPath)

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	return doInstallSoftware(cfg, installVMName, installSkipCNI, installCluster)
}

func doInstallSoftware(cfg *config.Config, vmName string, skipCNI bool, clusterName string) error {
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
		return installOnKind(cfg, skipCNI, clusterName)
	default:
		return fmt.Errorf("unknown deployment mode: %s", mode)
	}
}

func installOnKind(cfg *config.Config, skipCNI bool, clusterName string) error {
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

		if err := cniMgr.InstallCNI(cniType, cluster.Name, ""); err != nil {
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
