package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/linux"
)

var (
	configPath   string
	noCleanup    bool
	parallel     bool
	mode         string
	skipDeploy   bool
	skipSoftware bool
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

For more control, use individual commands:
  - deploy-vm: Deploy VM-based infrastructure
  - deploy-kind: Deploy Kind-based clusters
  - install-software: Install Kubernetes and CNI
  - vmctl: Manage VMs`,
	RunE: runDeploy,
}

func init() {
	rootCmd.Flags().StringVar(&configPath, "config", "config.yaml", "Path to configuration file")
	rootCmd.Flags().BoolVar(&noCleanup, "no-cleanup", false, "Skip cleanup of existing resources before deployment")
	rootCmd.Flags().BoolVar(&parallel, "parallel", false, "Install software on all VMs in parallel (VM mode only)")
	rootCmd.Flags().StringVarP(&mode, "mode", "m", "auto", "Deployment mode: auto, vm, or kind")
	rootCmd.Flags().BoolVar(&skipDeploy, "skip-deploy", false, "Skip infrastructure deployment")
	rootCmd.Flags().BoolVar(&skipSoftware, "skip-software", false, "Skip software installation")
}

func runDeploy(cmd *cobra.Command, args []string) error {
	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║         DPU Simulator - Go Edition           ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Printf("\nConfiguration: %s\n", configPath)

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Ensure dpu-sim dependencies are installed
	fmt.Println("\n=== Checking Dependencies ===")
	if err := linux.EnsureDependencies(cfg); err != nil {
		return fmt.Errorf("dependency check failed: %w", err)
	}

	// Determine deployment mode
	var deployMode string
	if mode == "auto" {
		deployMode, err = cfg.GetDeploymentMode()
		if err != nil {
			return fmt.Errorf("failed to determine deployment mode: %w", err)
		}
	} else {
		deployMode = mode
	}

	fmt.Printf("Deployment mode: %s\n", deployMode)
	fmt.Printf("Cleanup: %v\n", !noCleanup)

	// Run deployment workflow
	switch deployMode {
	case "vm":
		return runVMDeployment()
	case "kind":
		return runKindDeployment()
	default:
		return fmt.Errorf("unknown deployment mode: %s", deployMode)
	}
}

func runVMDeployment() error {
	fmt.Println("\n╔═══════════════════════════════════════════════╗")
	fmt.Println("║       VM-Based Deployment Workflow           ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")

	// Step 1: Deploy VMs
	if !skipDeploy {
		fmt.Println("\n[Step 1/2] Deploying VMs...")
		if err := runCommand("deploy-vm"); err != nil {
			return fmt.Errorf("VM deployment failed: %w", err)
		}
	} else {
		fmt.Println("\n[Step 1/2] Skipping VM deployment")
	}

	// Step 2: Install software
	if !skipSoftware {
		fmt.Println("\n[Step 2/2] Installing Kubernetes and CNI...")
		if err := runCommand("install-software"); err != nil {
			return fmt.Errorf("software installation failed: %w", err)
		}
	} else {
		fmt.Println("\n[Step 2/2] Skipping software installation")
	}

	printSuccessMessage("VM")
	return nil
}

func runKindDeployment() error {
	fmt.Println("\n╔═══════════════════════════════════════════════╗")
	fmt.Println("║      Kind-Based Deployment Workflow          ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")

	// Step 1: Deploy Kind clusters
	if !skipDeploy {
		fmt.Println("\n[Step 1/2] Deploying Kind clusters...")
		if err := runCommand("deploy-kind"); err != nil {
			return fmt.Errorf("Kind deployment failed: %w", err)
		}
	} else {
		fmt.Println("\n[Step 1/2] Skipping Kind deployment")
	}

	// Step 2: Install CNI
	if !skipSoftware {
		fmt.Println("\n[Step 2/2] Installing CNI...")
		if err := runCommand("install-software"); err != nil {
			return fmt.Errorf("CNI installation failed: %w", err)
		}
	} else {
		fmt.Println("\n[Step 2/2] Skipping CNI installation")
	}

	printSuccessMessage("Kind")
	return nil
}

func runCommand(cmdName string) error {
	// Find the command in the same directory as dpu-sim
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	binDir := filepath.Dir(execPath)
	cmdPath := filepath.Join(binDir, cmdName)

	// Build command arguments
	args := []string{"--config", configPath}
	if noCleanup {
		args = append(args, "--no-cleanup")
	}
	if parallel && cmdName == "install-software" {
		args = append(args, "--parallel")
	}

	// Execute command
	cmd := exec.Command(cmdPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

func printSuccessMessage(deployType string) {
	fmt.Println("\n╔═══════════════════════════════════════════════╗")
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
		fmt.Println("  kubectl --kubeconfig kubeconfig/<cluster>.yaml get nodes")
	} else {
		fmt.Println("\n✓ Kind deployment complete!")
		fmt.Println("\nYour DPU simulation environment is ready:")
		fmt.Println("  • Kind clusters are running")
		fmt.Println("  • CNI is deployed and ready")
		fmt.Println("\nUseful commands:")
		fmt.Println("  kind get clusters             # List all clusters")
		fmt.Println("  kubectl --kubeconfig kubeconfig/<cluster>.yaml get nodes")
		fmt.Println("  kubectl --kubeconfig kubeconfig/<cluster>.yaml get pods -A")
	}

	fmt.Println("\nKubeconfig files: ./kubeconfig/")
	fmt.Println("\nFor more information, see README-GO.md")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
