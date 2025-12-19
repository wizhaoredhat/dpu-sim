package vm

import (
	"fmt"
	"os/exec"
	"strings"

	"libvirt.org/go/libvirt"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/network"
)

// NetworkExists checks if a network exists
func NetworkExists(conn *libvirt.Connect, networkName string) bool {
	net, err := conn.LookupNetworkByName(networkName)
	if err != nil {
		return false
	}
	defer net.Free()
	return true
}

// CreateNetwork creates a libvirt network based on the configuration
func CreateNetwork(conn *libvirt.Connect, cfg *config.Config, netCfg config.NetworkConfig) error {
	if NetworkExists(conn, netCfg.Name) {
		fmt.Printf("Network %s already exists, skipping creation\n", netCfg.Name)
		return nil
	}

	// Generate network XML based on Mode
	var xml string

	switch netCfg.Mode {
	case "nat":
		xml = generateNATNetworkXML(cfg, netCfg)
	case "l2-bridge":
		if netCfg.UseOVS {
			// For OVS networks, create the OVS bridge first, then the libvirt network
			if err := CreateOVSBridge(netCfg.BridgeName); err != nil {
				return fmt.Errorf("failed to create OVS bridge %s: %w", netCfg.BridgeName, err)
			}
			xml = generateOVSNetworkXML(netCfg.Name, netCfg.BridgeName)
		} else {
			xml = generateBridgeNetworkXML(netCfg.Name, netCfg.BridgeName)
		}
	default:
		return fmt.Errorf("unsupported network mode: %s", netCfg.Mode)
	}

	net, err := conn.NetworkDefineXML(xml)
	if err != nil {
		return fmt.Errorf("failed to define network %s: %w", netCfg.Name, err)
	}
	defer net.Free()

	if err := net.SetAutostart(true); err != nil {
		return fmt.Errorf("failed to set autostart for network %s: %w", netCfg.Name, err)
	}

	if err := net.Create(); err != nil {
		return fmt.Errorf("failed to start network %s: %w", netCfg.Name, err)
	}

	fmt.Printf("✓ Created network: %s\n", netCfg.Name)
	return nil
}

func buildDHCPReservations(cfg *config.Config) string {
	var sb strings.Builder
	for _, vmCfg := range cfg.VMs {
		if vmCfg.K8sNodeMAC != "" && vmCfg.K8sNodeIP != "" {
			sb.WriteString(fmt.Sprintf("      <host mac='%s' name='%s' ip='%s'/>\n", vmCfg.K8sNodeMAC, vmCfg.Name, vmCfg.K8sNodeIP))
		}
	}
	return sb.String()
}

// generateNATNetworkXML generates XML for a NAT network with Linux bridges and DHCP
func generateNATNetworkXML(cfg *config.Config, netCfg config.NetworkConfig) string {
	var sb strings.Builder

	sb.WriteString("<network>\n")
	sb.WriteString(fmt.Sprintf("  <name>%s</name>\n", netCfg.Name))
	sb.WriteString("  <forward mode='nat'/>\n")
	sb.WriteString(fmt.Sprintf("  <bridge name='%s' stp='on' delay='0'/>\n", netCfg.BridgeName))

	sb.WriteString(fmt.Sprintf("  <ip address='%s' netmask='%s'>\n", netCfg.Gateway, netCfg.SubnetMask))

	sb.WriteString("    <dhcp>\n")
	if netCfg.DHCPStart != "" && netCfg.DHCPEnd != "" {
		sb.WriteString(fmt.Sprintf("      <range start='%s' end='%s'/>\n", netCfg.DHCPStart, netCfg.DHCPEnd))
	}
	sb.WriteString(buildDHCPReservations(cfg))
	sb.WriteString("    </dhcp>\n")

	sb.WriteString("  </ip>\n")

	sb.WriteString("</network>\n")
	return sb.String()
}

// generateBridgeNetworkXML generates XML for a simple Linux bridge network
func generateBridgeNetworkXML(networkName, bridgeName string) string {
	var sb strings.Builder

	sb.WriteString("<network>\n")
	sb.WriteString(fmt.Sprintf("  <name>%s</name>\n", networkName))
	sb.WriteString(fmt.Sprintf("  <bridge name='%s' stp='on' delay='0'/>\n", bridgeName))
	sb.WriteString("</network>\n")

	return sb.String()
}

// generateOVSNetworkXML generates XML for an OVS-based bridge network
func generateOVSNetworkXML(networkName, bridgeName string) string {
	var sb strings.Builder

	sb.WriteString("<network>\n")
	sb.WriteString(fmt.Sprintf("  <name>%s</name>\n", networkName))
	sb.WriteString("  <forward mode='bridge'/>\n")
	sb.WriteString(fmt.Sprintf("  <bridge name='%s'/>\n", bridgeName))
	sb.WriteString("  <virtualport type='openvswitch'/>\n")
	sb.WriteString("</network>\n")

	return sb.String()
}

// CreateOVSBridge creates an OVS bridge
func CreateOVSBridge(bridgeName string) error {
	checkCmd := exec.Command("ovs-vsctl", "br-exists", bridgeName)
	if err := checkCmd.Run(); err == nil {
		fmt.Printf("OVS bridge %s already exists, skipping creation\n", bridgeName)
		return nil
	}

	createCmd := exec.Command("ovs-vsctl", "add-br", bridgeName)
	if output, err := createCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create OVS bridge %s: %w, output: %s", bridgeName, err, string(output))
	}

	bringUpCmd := exec.Command("ip", "link", "set", bridgeName, "up")
	if output, err := bringUpCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to bring up OVS bridge %s: %w, output: %s", bridgeName, err, string(output))
	}

	fmt.Printf("✓ Created OVS bridge: %s\n", bridgeName)
	return nil
}

// DeleteOVSBridge deletes an OVS bridge
func DeleteOVSBridge(bridgeName string) error {
	checkCmd := exec.Command("ovs-vsctl", "br-exists", bridgeName)
	if err := checkCmd.Run(); err != nil {
		fmt.Printf("OVS bridge %s doesn't exist, skipping deletion\n", bridgeName)
		return nil
	}

	deleteCmd := exec.Command("ovs-vsctl", "del-br", bridgeName)
	if output, err := deleteCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to delete OVS bridge %s: %w, output: %s", bridgeName, err, string(output))
	}

	return nil
}

// CreateHostToDPUNetwork creates a network for host-to-DPU connection.
// Currently this is hardcoded to usee OvS for the bridge.
func CreateHostToDPUNetwork(conn *libvirt.Connect, hostName, dpuName string) error {
	networkName := network.GetHostToDPUNetworkName(hostName, dpuName)
	bridgeName := network.GenerateBridgeName(hostName, dpuName)

	if NetworkExists(conn, networkName) {
		return fmt.Errorf("host-to-DPU network %s already exists", networkName)
	}

	if err := CreateOVSBridge(bridgeName); err != nil {
		return fmt.Errorf("failed to create OVS bridge for host-to-DPU: %w", err)
	}

	xml := generateOVSNetworkXML(networkName, bridgeName)

	net, err := conn.NetworkDefineXML(xml)
	if err != nil {
		return fmt.Errorf("failed to define host-to-DPU network %s: %w", networkName, err)
	}
	defer net.Free()

	if err := net.SetAutostart(true); err != nil {
		return fmt.Errorf("failed to set autostart for network %s: %w", networkName, err)
	}

	if err := net.Create(); err != nil {
		return fmt.Errorf("failed to start network %s: %w", networkName, err)
	}

	fmt.Printf("✓ Created host-to-DPU network: %s (bridge: %s)\n", networkName, bridgeName)
	return nil
}

// CreateAllNetworks creates all networks defined in the configuration and
// implicit host to DPU networks.
func CreateAllNetworks(cfg *config.Config, conn *libvirt.Connect) error {
	fmt.Println("=== Creating Networks ===")

	// Create configured networks
	for _, netCfg := range cfg.Networks {
		if err := CreateNetwork(conn, cfg, netCfg); err != nil {
			return fmt.Errorf("failed to create network %s: %w", netCfg.Name, err)
		}
	}

	// Create implicit host to DPU networks
	mappings := cfg.GetHostDPUMappings()
	for _, mapping := range mappings {
		for _, dpuConn := range mapping.Connections {
			if err := CreateHostToDPUNetwork(conn, mapping.Host.Name, dpuConn.DPU.Name); err != nil {
				return fmt.Errorf("failed to create host-to-DPU network for host %s and DPU %s: %w",
					mapping.Host.Name, dpuConn.DPU.Name, err)
			}
		}
	}

	fmt.Println("✓ All networks created successfully")
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

	active, err := net.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check if network is active: %w", err)
	}

	if active {
		if err := net.Destroy(); err != nil {
			return fmt.Errorf("failed to destroy network: %w", err)
		}
	}

	if err := net.Undefine(); err != nil {
		return fmt.Errorf("failed to undefine network: %w", err)
	}

	return nil
}

// CleanupNetworks removes all networks defined in the configuration
func CleanupNetworks(cfg *config.Config, conn *libvirt.Connect) error {
	fmt.Println("=== Cleaning up Networks ===")

	errors := make([]string, 0)

	// Cleanup configured networks
	for _, netCfg := range cfg.Networks {
		netName := netCfg.Name
		fmt.Printf("Cleaning up network: %s... ", netName)

		if err := DeleteNetwork(conn, netName); err != nil {
			fmt.Printf("✗ Failed to remove network %s: %v\n", netName, err)
			errors = append(errors, fmt.Sprintf("failed to remove network %s: %v", netName, err))
			continue
		}

		if netCfg.UseOVS {
			fmt.Printf("Cleaning up OVS bridge: %s... ", netCfg.BridgeName)
			if err := DeleteOVSBridge(netCfg.BridgeName); err != nil {
				fmt.Printf("✗ Failed to remove OVS bridge %s: %v\n", netCfg.BridgeName, err)
				errors = append(errors, fmt.Sprintf("failed to remove OVS bridge %s: %v", netCfg.BridgeName, err))
				continue
			}
		}

		fmt.Printf("✓ Removed network %s\n", netName)
	}

	// Cleanup implicit host-to-DPU networks
	mappings := cfg.GetHostDPUMappings()
	for _, mapping := range mappings {
		for _, dpuConn := range mapping.Connections {
			netName := dpuConn.Link.NetworkName
			bridgeName := network.GenerateBridgeName(mapping.Host.Name, dpuConn.DPU.Name)
			fmt.Printf("Cleaning up host-to-DPU network: %s... ", netName)

			if err := DeleteNetwork(conn, netName); err != nil {
				fmt.Printf("✗ Failed to remove network %s: %v\n", netName, err)
				errors = append(errors, fmt.Sprintf("failed to remove network %s: %v", netName, err))
				continue
			}

			fmt.Printf("Cleaning up host-to-DPU OVS bridge: %s... ", bridgeName)
			if err := DeleteOVSBridge(bridgeName); err != nil {
				fmt.Printf("✗ Failed to remove OVS bridge %s: %v\n", bridgeName, err)
				errors = append(errors, fmt.Sprintf("failed to remove OVS bridge %s: %v", bridgeName, err))
				continue
			}

			fmt.Printf("✓ Removed host-to-DPU network %s (bridge: %s)\n", netName, bridgeName)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup networks errors: %s", strings.Join(errors, "; "))
	}

	return nil
}
