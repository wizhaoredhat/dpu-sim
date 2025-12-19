package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/ssh"
	"github.com/wizhao/dpu-sim/pkg/vm"
)

var (
	configPath string
	cleanup    bool
)

var rootCmd = &cobra.Command{
	Use:   "deploy-vm",
	Short: "Deploy VMs with libvirt",
	Long:  `Deploy virtual machines with libvirt network for DPU simulation`,
	RunE:  runVMDeploy,
}

func init() {
	rootCmd.Flags().StringVar(&configPath, "config", "config.yaml", "Path to configuration file")
	rootCmd.Flags().BoolVar(&cleanup, "cleanup", false, "Only run cleanup, do not deploy")
}

func runVMDeploy(cmd *cobra.Command, args []string) error {
	fmt.Println("=== DPU Simulator - VM Deployment ===")
	fmt.Printf("Configuration: %s\n", configPath)

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	conn, err := vm.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to libvirt: %w", err)
	}
	defer conn.Close()

	fmt.Println("\n=== Cleaning up VMs and networks ===")
	if err := vm.CleanupAll(cfg, conn); err != nil {
		fmt.Printf("Warning: cleanup failed: %v\n", err)
	}

	if cleanup {
		fmt.Println("\n=== Cleanup complete. No deployment performed. ===")
		return nil
	}

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

	fmt.Println("\n=== Deployment Complete ===")
	fmt.Println("All VMs are running and accessible via SSH")
	fmt.Println("\nNext steps:")
	fmt.Println("  - Use 'vmctl list' to see VM status")
	fmt.Println("  - Use 'vmctl ssh <vm-name>' to connect to VMs")
	fmt.Println("  - Run 'install-software' to install Kubernetes and CNI components")

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
