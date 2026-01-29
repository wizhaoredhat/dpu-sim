package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
	"github.com/wizhao/dpu-sim/pkg/ssh"
	"github.com/wizhao/dpu-sim/pkg/vm"
)

var (
	configPath string
	logLevel   string
)

var rootCmd = &cobra.Command{
	Use:   "vmctl",
	Short: "VM Control Utility",
	Long:  `Manage and interact with DPU simulation VMs`,
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all VMs",
	RunE:  runList,
}

var sshCmd = &cobra.Command{
	Use:   "ssh <vm-name>",
	Short: "SSH into a VM",
	Args:  cobra.ExactArgs(1),
	RunE:  runSSH,
}

var startCmd = &cobra.Command{
	Use:   "start <vm-name>",
	Short: "Start a VM",
	Args:  cobra.ExactArgs(1),
	RunE:  runStart,
}

var stopCmd = &cobra.Command{
	Use:   "stop <vm-name>",
	Short: "Stop a VM",
	Args:  cobra.ExactArgs(1),
	RunE:  runStop,
}

var destroyCmd = &cobra.Command{
	Use:   "destroy <vm-name>",
	Short: "Force stop a VM",
	Args:  cobra.ExactArgs(1),
	RunE:  runDestroy,
}

var rebootCmd = &cobra.Command{
	Use:   "reboot <vm-name>",
	Short: "Reboot a VM",
	Args:  cobra.ExactArgs(1),
	RunE:  runReboot,
}

var execCmd = &cobra.Command{
	Use:   "exec <vm-name> <command> [args...]",
	Short: "Execute a command on a VM via SSH",
	Long:  `Execute a single command on a VM via SSH and print the output.`,
	Args:  cobra.MinimumNArgs(2),
	RunE:  runExec,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "config.yaml", "Path to configuration file")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info",
		fmt.Sprintf("Log level (%s)", strings.Join(log.ValidLevels(), ", ")))

	// Initialize log level in PersistentPreRun
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		log.SetLevel(log.ParseLevel(logLevel))
	}

	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(sshCmd)
	rootCmd.AddCommand(execCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(destroyCmd)
	rootCmd.AddCommand(rebootCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	vmMgr, err := vm.NewVMManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create VM manager: %w", err)
	}
	defer vmMgr.Close()

	log.Info("%-20s %-15s %-15s %-8s %-10s", "VM Name", "State", "IP Address", "vCPUs", "Memory")
	log.Info("--------------------------------------------------------------------------------")

	for _, vmCfg := range cfg.VMs {
		vmName := vmCfg.Name

		info, err := vmMgr.GetVMInfo(vmName, config.MgmtNetworkName)
		if err != nil {
			// VM doesn't exist
			log.Info("%-20s %-15s %-15s %-8s %-10s", vmName, "Not Found", "N/A", "N/A", "N/A")
			continue
		}

		ipAddr := info.IP
		if ipAddr == "" {
			ipAddr = "N/A"
		}

		log.Info("%-20s %-15s %-15s %-8d %dMB",
			info.Name, info.State, ipAddr, info.VCPUs, info.MemoryMB)
	}

	return nil
}

func runSSH(cmd *cobra.Command, args []string) error {
	vmName := args[0]

	// Load config
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create VM manager
	vmMgr, err := vm.NewVMManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create VM manager: %w", err)
	}
	defer vmMgr.Close()

	// Get VM IP
	ip, err := vmMgr.GetVMMgmtIP(vmName)
	if err != nil {
		return fmt.Errorf("failed to get IP for VM %s: %w", vmName, err)
	}

	log.Info("Connecting to %s (%s) as %s...", vmName, ip, cfg.SSH.User)

	// Build SSH command and execute
	sshCmd := ssh.BuildSSHCommand(&cfg.SSH, ip, "")
	sshExec := exec.Command(sshCmd[0], sshCmd[1:]...)
	sshExec.Stdin = os.Stdin
	sshExec.Stdout = os.Stdout
	sshExec.Stderr = os.Stderr

	return sshExec.Run()
}

func runExec(cmd *cobra.Command, args []string) error {
	vmName := args[0]
	command := strings.Join(args[1:], " ")

	// Load config
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create VM manager
	vmMgr, err := vm.NewVMManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create VM manager: %w", err)
	}
	defer vmMgr.Close()

	// Get VM IP
	ip, err := vmMgr.GetVMMgmtIP(vmName)
	if err != nil {
		return fmt.Errorf("failed to get IP for VM %s: %w", vmName, err)
	}

	// Execute command via SSH
	cmdExec := platform.NewSSHExecutor(&cfg.SSH, ip)
	if err := cmdExec.WaitUntilReady(10 * time.Second); err != nil {
		return fmt.Errorf("failed to wait for SSH on %s: %w", vmName, err)
	}
	stdout, stderr, err := cmdExec.Execute(command)

	// Print output
	if stdout != "" {
		fmt.Print(stdout)
	}
	if stderr != "" {
		fmt.Fprint(os.Stderr, stderr)
	}

	if err != nil {
		return fmt.Errorf("command failed: %w", err)
	}

	return nil
}

func runStart(cmd *cobra.Command, args []string) error {
	vmName := args[0]

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	vmMgr, err := vm.NewVMManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create VM manager: %w", err)
	}
	defer vmMgr.Close()

	if err := vmMgr.StartVM(vmName); err != nil {
		return err
	}

	log.Info("✓ Started VM '%s'", vmName)
	return nil
}

func runStop(cmd *cobra.Command, args []string) error {
	vmName := args[0]

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	vmMgr, err := vm.NewVMManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create VM manager: %w", err)
	}
	defer vmMgr.Close()

	if err := vmMgr.StopVM(vmName); err != nil {
		return err
	}

	log.Info("✓ Shutting down VM '%s'...", vmName)
	return nil
}

func runDestroy(cmd *cobra.Command, args []string) error {
	vmName := args[0]

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	vmMgr, err := vm.NewVMManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create VM manager: %w", err)
	}
	defer vmMgr.Close()

	if err := vmMgr.DestroyVM(vmName); err != nil {
		return err
	}

	log.Info("✓ Force stopped VM '%s'", vmName)
	return nil
}

func runReboot(cmd *cobra.Command, args []string) error {
	vmName := args[0]

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	vmMgr, err := vm.NewVMManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create VM manager: %w", err)
	}
	defer vmMgr.Close()

	if err := vmMgr.RebootVM(vmName); err != nil {
		return err
	}

	log.Info("✓ Rebooting VM '%s'...", vmName)
	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
