package vm

import (
	"fmt"
	"strings"

	"libvirt.org/go/libvirt"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/network"
)

// CleanupVMs removes all VMs defined in the configuration
func CleanupVMs(cfg *config.Config, conn *libvirt.Connect) error {
	fmt.Println("=== Cleaning up VMs ===")

	for _, vmCfg := range cfg.VMs {
		vmName := vmCfg.Name
		fmt.Printf("Checking VM: %s... ", vmName)

		if err := DeleteVM(conn, vmName); err != nil {
			fmt.Printf("✗ Failed: %v\n", err)
			// Continue with other VMs
			continue
		}

		fmt.Println("✓ Removed")
	}

	return nil
}

// CleanupNetworks removes all networks defined in the configuration
func CleanupNetworks(cfg *config.Config, conn *libvirt.Connect) error {
	fmt.Println("=== Cleaning up Networks ===")

	// Cleanup configured networks
	for _, netCfg := range cfg.Networks {
		netName := netCfg.Name
		fmt.Printf("Checking network: %s... ", netName)

		if err := DeleteNetwork(conn, netName); err != nil {
			fmt.Printf("✗ Failed: %v\n", err)
			continue
		}

		fmt.Println("✓ Removed")
	}

	// Cleanup host-to-DPU networks
	pairs := cfg.GetHostDPUPairs()
	for _, pair := range pairs {
		netName := network.GetHostToDPUNetworkName(pair.Host.Name, pair.DPU.Name)
		fmt.Printf("Checking host-to-DPU network: %s... ", netName)

		if err := DeleteNetwork(conn, netName); err != nil {
			fmt.Printf("✗ Failed: %v\n", err)
			continue
		}

		fmt.Println("✓ Removed")
	}

	return nil
}

// DeleteNetwork removes a libvirt network by name
func DeleteNetwork(conn *libvirt.Connect, networkName string) error {
	net, err := conn.LookupNetworkByName(networkName)
	if err != nil {
		// Network doesn't exist, nothing to do
		return nil
	}
	defer net.Free()

	// Check if network is active
	active, err := net.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check if network is active: %w", err)
	}

	// Stop network if active
	if active {
		if err := net.Destroy(); err != nil {
			return fmt.Errorf("failed to destroy network: %w", err)
		}
	}

	// Undefine the network
	if err := net.Undefine(); err != nil {
		return fmt.Errorf("failed to undefine network: %w", err)
	}

	return nil
}

// CleanupOVSBridges removes OVS bridges created for the simulation
func CleanupOVSBridges(cfg *config.Config) error {
	fmt.Println("=== Cleaning up OVS Bridges ===")

	// Cleanup bridges from configured networks
	for _, netCfg := range cfg.Networks {
		if !netCfg.UseOVS {
			continue
		}

		bridgeName := netCfg.BridgeName
		fmt.Printf("Checking OVS bridge: %s... ", bridgeName)

		if err := deleteOVSBridge(bridgeName); err != nil {
			fmt.Printf("✗ Failed: %v\n", err)
			continue
		}

		fmt.Println("✓ Removed")
	}

	// Cleanup host-to-DPU bridges
	pairs := cfg.GetHostDPUPairs()
	for _, pair := range pairs {
		bridgeName := network.GenerateBridgeName(pair.Host.Name, pair.DPU.Name)
		fmt.Printf("Checking host-to-DPU OVS bridge: %s... ", bridgeName)

		if err := deleteOVSBridge(bridgeName); err != nil {
			fmt.Printf("✗ Failed: %v\n", err)
			continue
		}

		fmt.Println("✓ Removed")
	}

	return nil
}

// deleteOVSBridge deletes an OVS bridge
func deleteOVSBridge(bridgeName string) error {
	return DeleteOVSBridge(bridgeName)
}

// NetworkExists checks if a network exists
func NetworkExists(conn *libvirt.Connect, networkName string) bool {
	net, err := conn.LookupNetworkByName(networkName)
	if err != nil {
		return false
	}
	defer net.Free()
	return true
}

// CleanupAll performs comprehensive cleanup of all resources
func CleanupAll(cfg *config.Config, conn *libvirt.Connect) error {
	errors := make([]string, 0)

	// Cleanup VMs
	if err := CleanupVMs(cfg, conn); err != nil {
		errors = append(errors, fmt.Sprintf("VM cleanup: %v", err))
	}

	// Cleanup networks
	if err := CleanupNetworks(cfg, conn); err != nil {
		errors = append(errors, fmt.Sprintf("Network cleanup: %v", err))
	}

	// Cleanup OVS bridges
	if err := CleanupOVSBridges(cfg); err != nil {
		errors = append(errors, fmt.Sprintf("OVS cleanup: %v", err))
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errors, "; "))
	}

	return nil
}
