package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/wizhao/dpu-sim/pkg/cni"
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/k8s"
	"github.com/wizhao/dpu-sim/pkg/kind"
	"github.com/wizhao/dpu-sim/pkg/platform"
	"github.com/wizhao/dpu-sim/pkg/requirements"
	"github.com/wizhao/dpu-sim/pkg/vm"
)

var (
	// Global flags
	configPath string

	// Root command flags
	skipDeps    bool
	skipCleanup bool
	cleanupOnly bool
	skipDeploy  bool
	skipK8s     bool
)

var rootCmd = &cobra.Command{
	Use:   "dpu-sim",
	Short: "DPU Simulator - Complete setup orchestration",
	Long: `DPU Simulator automates deployment of DPU simulation environments
using either VMs (libvirt) or containers (Kind), pre-configured with
Kubernetes and CNI for container networking experiments.

This is the main orchestrator that runs the complete deployment workflow:
  1. Install dependencies
  2. Clean up existing resources (Idempotent deployment - can be run multiple times safely)
  3. Deploy infrastructure (VMs or Kind clusters)
  4. Install Kubernetes and CNI components`,
	RunE: runDeploy,
}

func init() {
	// Global persistent flag
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "config.yaml", "Path to configuration file")

	// Root command flags
	rootCmd.Flags().BoolVar(&cleanupOnly, "cleanup", false, "Only cleanup existing resources, do not deploy")
	rootCmd.Flags().BoolVar(&skipDeps, "skip-deps", false, "Skip dependency checks")
	rootCmd.Flags().BoolVar(&skipCleanup, "skip-cleanup", false, "Skip cleanup of existing resources")
	rootCmd.Flags().BoolVar(&skipDeploy, "skip-deploy", false, "Skip VM/Kind deployment")
	rootCmd.Flags().BoolVar(&skipK8s, "skip-k8s", false, "Skip Kubernetes (VM only) and CNI installation")
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
		if err := requirements.EnsureDependencies(cfg); err != nil {
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

	if !skipCleanup || cleanupOnly {
		fmt.Println("\n=== Cleaning up K8s ===")
		if err := k8s.CleanupAll(cfg); err != nil {
			fmt.Printf("Warning: Kubernetes cleanup failed: %v\n", err)
		}
	} else {
		fmt.Println("\nSkipping Kubernetes cleanup")
	}

	switch deployMode {
	case config.VMDeploymentMode:
		return runVMDeploymentWorkflow(cfg)
	case config.KindDeploymentMode:
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

	vmMgr, err := vm.NewVMManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create VM manager: %w", err)
	}
	defer vmMgr.Close()

	if !skipCleanup || cleanupOnly {
		fmt.Println("\n=== Cleaning up VMs and networks ===")
		if err := vmMgr.CleanupAll(); err != nil {
			fmt.Printf("Warning: cleanup failed: %v\n", err)
		}
		if cleanupOnly {
			fmt.Println("\n✓ Cleanup complete. No deployment performed.")
			return nil
		}
	}

	if !skipDeploy {
		fmt.Println("\n=== Deploying VMs ===")
		if err := doVMDeploy(cfg, vmMgr); err != nil {
			return fmt.Errorf("VM deployment failed: %w", err)
		}
	} else {
		fmt.Println("\nSkipping VM deployment")
	}

	if !skipK8s {
		fmt.Println("\n=== Installing Kubernetes and CNI ===")
		if err := doVMInstallK8s(vmMgr); err != nil {
			return fmt.Errorf("Kubernetes installation failed: %w", err)
		}
	} else {
		fmt.Println("\nSkipping Kubernetes installation")
	}

	printSuccessMessage(cfg, "VM")
	return nil
}

func runKindDeploymentWorkflow(cfg *config.Config) error {
	fmt.Println("")
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║      Kind-Based Deployment Workflow           ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")

	kindMgr := kind.NewKindManager(cfg)

	if !skipCleanup || cleanupOnly {
		fmt.Println("\n=== Cleaning up existing kind clusters ===")
		if err := kindMgr.CleanupAll(cfg); err != nil {
			return fmt.Errorf("failed to cleanup Kind clusters: %w", err)
		}
		if cleanupOnly {
			fmt.Println("✓ Cleanup complete")
			return nil
		}
	}

	if !skipDeploy {
		fmt.Println("\n=== Deploying Kind clusters ===")
		if err := doKindDeploy(cfg, kindMgr); err != nil {
			return fmt.Errorf("Kind deployment failed: %w", err)
		}
	} else {
		fmt.Println("\nSkipping Kind deployment")
	}

	if !skipK8s {
		fmt.Println("\n=== Installing CNI ===")
		if err := doKindInstallCNI(cfg); err != nil {
			return fmt.Errorf("CNI installation failed: %w", err)
		}
	} else {
		fmt.Println("\nSkipping CNI installation")
	}

	printSuccessMessage(cfg, "Kind")
	return nil
}

func printSuccessMessage(cfg *config.Config, deployType string) {
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
		fmt.Printf("  kubectl --kubeconfig %s/<cluster>.kubeconfig get nodes\n", cfg.Kubernetes.GetKubeconfigDir())
	} else {
		fmt.Println("\n✓ Kind deployment complete!")
		fmt.Println("\nYour DPU simulation environment is ready:")
		fmt.Println("  • Kind clusters are running")
		fmt.Println("  • CNI is deployed and ready")
		fmt.Println("\nUseful commands:")
		fmt.Println("  kind get clusters             # List all clusters")
		fmt.Printf("  kubectl --kubeconfig %s/<cluster>.kubeconfig get nodes\n", cfg.Kubernetes.GetKubeconfigDir())
	}

	fmt.Printf("\nKubeconfig files: %s\n", cfg.Kubernetes.GetKubeconfigDir())
	fmt.Println("\nFor more information, see README.md")
}

func doVMDeploy(cfg *config.Config, vmMgr *vm.VMManager) error {
	// Create networks
	if err := vmMgr.CreateAllNetworks(); err != nil {
		return fmt.Errorf("failed to create networks: %w", err)
	}

	// Create VMs
	if err := vmMgr.CreateAllVMs(); err != nil {
		return fmt.Errorf("failed to create VMs: %w", err)
	}

	// Wait for VMs to get IP addresses
	fmt.Println("\n=== Waiting for VMs to boot and get IPs ===")
	for _, vmCfg := range cfg.VMs {
		fmt.Printf("Waiting for %s to get an IP address...\n", vmCfg.Name)
		ip, err := vmMgr.WaitForVMIP(vmCfg.Name, config.MgmtNetworkName, 5*time.Minute)
		if err != nil {
			return fmt.Errorf("failed to get IP for %s: %w", vmCfg.Name, err)
		}
		fmt.Printf("✓ %s IP: %s\n", vmCfg.Name, ip)

		cmdExec := platform.NewSSHExecutor(&cfg.SSH, ip)
		fmt.Printf("Waiting for SSH on %s...\n", vmCfg.Name)
		if err := cmdExec.WaitUntilReady(5 * time.Minute); err != nil {
			return fmt.Errorf("failed to wait for SSH on %s: %w", vmCfg.Name, err)
		}
		fmt.Printf("✓ SSH ready on %s\n", vmCfg.Name)
	}

	return nil
}

func doVMInstallK8s(vmMgr *vm.VMManager) error {
	if err := vmMgr.InstallKubernetes(""); err != nil {
		return fmt.Errorf("failed to install Kubernetes: %w", err)
	}

	if err := vmMgr.SetupAllK8sClusters(); err != nil {
		return fmt.Errorf("failed to setup Kubernetes clusters: %w", err)
	}

	return nil
}

func doKindDeploy(cfg *config.Config, kindMgr *kind.KindManager) error {
	fmt.Println("\n=== Creating Kind Clusters ===")
	if err := kindMgr.DeployAllClusters(); err != nil {
		return fmt.Errorf("failed to deploy Kind clusters: %w", err)
	}

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

	return nil
}

func doKindInstallCNI(cfg *config.Config) error {
	fmt.Println("\n=== Installing CNI on Kind clusters ===")

	for _, cluster := range cfg.Kubernetes.Clusters {
		fmt.Printf("\n--- Installing CNI on cluster %s ---\n", cluster.Name)
		kubeconfigPath := k8s.GetKubeconfigPath(cluster.Name, cfg.Kubernetes.GetKubeconfigDir())
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
