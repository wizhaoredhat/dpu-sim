package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wizhao/dpu-sim/pkg/cni"
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/k8s"
	"github.com/wizhao/dpu-sim/pkg/kind"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
	"github.com/wizhao/dpu-sim/pkg/requirements"
	"github.com/wizhao/dpu-sim/pkg/vm"
)

var (
	// Global flags
	configPath string
	logLevel   string

	// Root command flags
	skipDeps    bool
	skipCleanup bool
	cleanupOnly bool
	skipDeploy  bool
	skipK8s     bool
	rebuildCNI  bool
	redeployCNI bool
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
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info",
		fmt.Sprintf("Log level (%s)", strings.Join(log.ValidLevels(), ", ")))

	// Root command flags
	rootCmd.Flags().BoolVar(&cleanupOnly, "cleanup", false, "Only cleanup existing resources, do not deploy")
	rootCmd.Flags().BoolVar(&skipDeps, "skip-deps", false, "Skip dependency checks")
	rootCmd.Flags().BoolVar(&skipCleanup, "skip-cleanup", false, "Skip cleanup of existing resources")
	rootCmd.Flags().BoolVar(&skipDeploy, "skip-deploy", false, "Skip VM/Kind deployment")
	rootCmd.Flags().BoolVar(&skipK8s, "skip-k8s", false, "Skip Kubernetes (VM only) and CNI installation")
	rootCmd.Flags().BoolVar(&rebuildCNI, "rebuild-cni", false, "Rebuild the OVN-Kubernetes CNI image and exit")
	rootCmd.Flags().BoolVar(&redeployCNI, "redeploy-cni", false, "Redeploy the OVN-Kubernetes CNI image onto each cluster and exit")
}

// =============================================================================
// Root command - Orchestrated deployment
// =============================================================================

func runDeploy(cmd *cobra.Command, args []string) error {
	// Configure log level
	log.SetLevel(log.ParseLevel(logLevel))

	log.Info("╔═══════════════════════════════════════════════╗")
	log.Info("║               DPU Simulator                   ║")
	log.Info("╚═══════════════════════════════════════════════╝")
	log.Info("Configuration: %s", configPath)

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Handle rebuild CNI image(s) and redeploy onto each cluster
	if rebuildCNI || redeployCNI {
		log.Info("\n=== Rebuilding CNI images ===")
		// Collect unique CNI types across all clusters
		seen := make(map[cni.CNIType]bool)
		for _, cluster := range cfg.Kubernetes.Clusters {
			seen[cni.CNIType(cluster.CNI)] = true
		}

		// Rebuild the image once per unique CNI type
		cniMgr, err := cni.NewCNIManager(cfg)
		if err != nil {
			return fmt.Errorf("failed to create CNI manager: %w", err)
		}
		for cniType := range seen {
			if err := cniMgr.RebuildCNIImage(cniType); err != nil {
				return fmt.Errorf("failed to rebuild CNI image for %s: %w", cniType, err)
			}
		}

		if redeployCNI {
			// Redeploy onto each cluster
			log.Info("\n=== Redeploying CNI images ===")
			kubeconfigDir := cfg.Kubernetes.GetKubeconfigDir()
			for _, cluster := range cfg.Kubernetes.Clusters {
				kubeconfigPath := k8s.GetKubeconfigPath(cluster.Name, kubeconfigDir)
				if err := cniMgr.SetKubeconfigFile(kubeconfigPath); err != nil {
					return fmt.Errorf("failed to set kubeconfig for cluster %s: %w", cluster.Name, err)
				}
				if err := cniMgr.RedeployCNI(cluster.Name); err != nil {
					return fmt.Errorf("failed to redeploy CNI on cluster %s: %w", cluster.Name, err)
				}
			}
		}

		return nil
	}

	deployMode, err := cfg.GetDeploymentMode()
	if err != nil {
		return fmt.Errorf("failed to determine deployment mode: %w", err)
	}
	log.Info("Deployment mode: %s", deployMode)

	if !skipDeps {
		log.Info("\n=== Checking Dependencies ===")
		if err := requirements.EnsureDependencies(cfg); err != nil {
			return fmt.Errorf("dependency check failed: %w", err)
		}
	} else {
		log.Info("\nSkipping dependency check")
	}

	if !skipCleanup || cleanupOnly {
		log.Info("\n=== Cleaning up K8s ===")
		if err := k8s.CleanupAll(cfg); err != nil {
			log.Warn("Warning: Kubernetes cleanup failed: %v", err)
		}
	} else {
		log.Info("\nSkipping Kubernetes cleanup")
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
	log.Info("")
	log.Info("╔═══════════════════════════════════════════════╗")
	log.Info("║       VM-Based Deployment Workflow            ║")
	log.Info("╚═══════════════════════════════════════════════╝")

	vmMgr, err := vm.NewVMManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create VM manager: %w", err)
	}
	defer vmMgr.Close()

	if !skipCleanup || cleanupOnly {
		if err := vmMgr.CleanupAll(); err != nil {
			log.Warn("Warning: cleanup failed: %v", err)
		}
		if cleanupOnly {
			log.Info("\n✓ Cleanup complete. No deployment performed.")
			return nil
		}
	}

	if !skipDeploy {
		log.Info("\n=== Deploying VMs ===")
		if err := doVMDeploy(cfg, vmMgr); err != nil {
			return fmt.Errorf("VM deployment failed: %w", err)
		}
	} else {
		log.Info("\nSkipping VM deployment")
	}

	if !skipK8s {
		log.Info("\n=== Installing Kubernetes and CNI ===")
		if err := doVMInstallK8s(vmMgr); err != nil {
			return fmt.Errorf("Kubernetes installation failed: %w", err)
		}
	} else {
		log.Info("\nSkipping Kubernetes installation")
	}

	printSuccessMessage(cfg, "VM")
	return nil
}

func runKindDeploymentWorkflow(cfg *config.Config) error {
	log.Info("")
	log.Info("╔═══════════════════════════════════════════════╗")
	log.Info("║      Kind-Based Deployment Workflow           ║")
	log.Info("╚═══════════════════════════════════════════════╝")

	kindMgr := kind.NewKindManager(cfg)

	if !skipCleanup || cleanupOnly {
		log.Info("\n=== Cleaning up existing kind clusters ===")
		if err := kindMgr.CleanupAll(cfg); err != nil {
			return fmt.Errorf("failed to cleanup Kind clusters: %w", err)
		}
		if cleanupOnly {
			log.Info("✓ Cleanup complete")
			return nil
		}
	}

	if !skipDeploy {
		log.Info("\n=== Deploying Kind clusters ===")
		if err := doKindDeploy(cfg, kindMgr); err != nil {
			return fmt.Errorf("Kind deployment failed: %w", err)
		}
	} else {
		log.Info("\nSkipping Kind deployment")
	}

	if !skipK8s {
		log.Info("\n=== Installing CNI ===")
		if err := doKindInstallCNI(kindMgr); err != nil {
			return fmt.Errorf("CNI installation failed: %w", err)
		}
	} else {
		log.Info("\nSkipping CNI installation")
	}

	printSuccessMessage(cfg, "Kind")
	return nil
}

func printSuccessMessage(cfg *config.Config, deployType string) {
	log.Info("")
	log.Info("╔═══════════════════════════════════════════════╗")
	log.Info("║         Deployment Completed Successfully!    ║")
	log.Info("╚═══════════════════════════════════════════════╝")

	if deployType == "VM" {
		log.Info("\n✓ VM deployment complete!")
		log.Info("\nYour DPU simulation environment is ready:")
		log.Info("  • VMs are running and accessible")
		log.Info("  • Kubernetes is installed and configured")
		log.Info("  • CNI is deployed and ready")
		log.Info("\nUseful commands:")
		log.Info("  vmctl list                    # List all VMs")
		log.Info("  vmctl ssh <vm-name>           # SSH into a VM")
		log.Info("  kubectl --kubeconfig %s/<cluster>.kubeconfig get nodes", cfg.Kubernetes.GetKubeconfigDir())
	} else {
		log.Info("\n✓ Kind deployment complete!")
		log.Info("\nYour DPU simulation environment is ready:")
		log.Info("  • Kind clusters are running")
		log.Info("  • CNI is deployed and ready")
		log.Info("\nUseful commands:")
		log.Info("  kind get clusters             # List all clusters")
		log.Info("  kubectl --kubeconfig %s/<cluster>.kubeconfig get nodes", cfg.Kubernetes.GetKubeconfigDir())
	}

	log.Info("\nKubeconfig files: %s", cfg.Kubernetes.GetKubeconfigDir())
	log.Info("For more information, see README.md")
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
	log.Info("\n=== Waiting for VMs to boot and get IPs ===")
	for _, vmCfg := range cfg.VMs {
		log.Info("Waiting for %s to get an IP address...", vmCfg.Name)
		ip, err := vmMgr.WaitForVMIP(vmCfg.Name, config.MgmtNetworkName, 5*time.Minute)
		if err != nil {
			return fmt.Errorf("failed to get IP for %s: %w", vmCfg.Name, err)
		}
		log.Info("✓ %s IP: %s", vmCfg.Name, ip)

		cmdExec := platform.NewSSHExecutor(&cfg.SSH, ip)
		log.Info("Waiting for SSH on %s...", vmCfg.Name)
		if err := cmdExec.WaitUntilReady(5 * time.Minute); err != nil {
			return fmt.Errorf("failed to wait for SSH on %s: %w", vmCfg.Name, err)
		}
		log.Info("✓ SSH ready on %s", vmCfg.Name)
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
	log.Info("\n=== Creating Kind Clusters ===")
	if err := kindMgr.DeployAllClusters(); err != nil {
		return fmt.Errorf("failed to deploy Kind clusters: %w", err)
	}

	for _, cluster := range cfg.Kubernetes.Clusters {
		info, err := kindMgr.GetClusterInfo(cluster.Name)
		if err != nil {
			log.Warn("Warning: failed to get info for %s: %v", cluster.Name, err)
			continue
		}

		log.Info("\nCluster: %s", info.Name)
		log.Info("  Status: %s", info.Status)
		log.Info("  Nodes:")
		for _, node := range info.Nodes {
			log.Info("    - %s (%s) [%s]", node.Name, node.Role, node.Status)
		}

		for _, node := range info.Nodes {
			dockerExec := platform.NewDockerExecutor(node.Name)
			if err := kindMgr.InstallDependencies(dockerExec); err != nil {
				return fmt.Errorf("failed to install Kind dependencies on %s: %w", node.Name, err)
			}
		}

	}

	return nil
}

func doKindInstallCNI(kindMgr *kind.KindManager) error {
	log.Info("\n=== Installing CNI on Kind clusters ===")

	if err := kindMgr.InstallCNI(); err != nil {
		return fmt.Errorf("failed to deploy CNI: %w", err)
	}
	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
