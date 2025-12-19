package vm

import (
	"fmt"
	"os/exec"
	"strings"

	"libvirt.org/go/libvirt"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/network"
)

// CreateNetwork creates a libvirt network based on the configuration
func CreateNetwork(conn *libvirt.Connect, netCfg config.NetworkConfig) error {
	// Check if network already exists
	if NetworkExists(conn, netCfg.Name) {
		fmt.Printf("Network %s already exists, skipping creation\n", netCfg.Name)
		return nil
	}

	// Generate network XML based on type
	var xml string
	var err error

	if netCfg.UseOVS {
		// For OVS networks, create the OVS bridge first, then the libvirt network
		if err := CreateOVSBridge(netCfg.BridgeName); err != nil {
			return fmt.Errorf("failed to create OVS bridge %s: %w", netCfg.BridgeName, err)
		}
		xml = generateOVSNetworkXML(netCfg)
	} else if netCfg.Type == "nat" {
		xml = generateNATNetworkXML(netCfg)
	} else if netCfg.Type == "bridge" || netCfg.Type == "l2" {
		xml = generateBridgeNetworkXML(netCfg)
	} else {
		return fmt.Errorf("unsupported network type: %s", netCfg.Type)
	}

	// Define the network
	net, err := conn.NetworkDefineXML(xml)
	if err != nil {
		return fmt.Errorf("failed to define network %s: %w", netCfg.Name, err)
	}
	defer net.Free()

	// Set autostart
	if err := net.SetAutostart(true); err != nil {
		return fmt.Errorf("failed to set autostart for network %s: %w", netCfg.Name, err)
	}

	// Start the network
	if err := net.Create(); err != nil {
		return fmt.Errorf("failed to start network %s: %w", netCfg.Name, err)
	}

	fmt.Printf("✓ Created network: %s\n", netCfg.Name)
	return nil
}

// generateNATNetworkXML generates XML for a NAT network with DHCP
func generateNATNetworkXML(netCfg config.NetworkConfig) string {
	var sb strings.Builder

	sb.WriteString("<network>\n")
	sb.WriteString(fmt.Sprintf("  <name>%s</name>\n", netCfg.Name))
	sb.WriteString("  <forward mode='nat'/>\n")
	sb.WriteString(fmt.Sprintf("  <bridge name='%s' stp='on' delay='0'/>\n", netCfg.BridgeName))

	// Add IP configuration if provided
	if netCfg.Subnet != "" {
		sb.WriteString(fmt.Sprintf("  <ip address='%s' netmask='%s'>\n", netCfg.Gateway, netCfg.Netmask))

		// Add DHCP range if specified
		if netCfg.DHCPStart != "" && netCfg.DHCPEnd != "" {
			sb.WriteString("    <dhcp>\n")
			sb.WriteString(fmt.Sprintf("      <range start='%s' end='%s'/>\n", netCfg.DHCPStart, netCfg.DHCPEnd))
			sb.WriteString("    </dhcp>\n")
		}

		sb.WriteString("  </ip>\n")
	}

	sb.WriteString("</network>\n")
	return sb.String()
}

// generateBridgeNetworkXML generates XML for a simple L2 bridge network
func generateBridgeNetworkXML(netCfg config.NetworkConfig) string {
	var sb strings.Builder

	sb.WriteString("<network>\n")
	sb.WriteString(fmt.Sprintf("  <name>%s</name>\n", netCfg.Name))
	sb.WriteString(fmt.Sprintf("  <bridge name='%s' stp='on' delay='0'/>\n", netCfg.BridgeName))
	sb.WriteString("</network>\n")

	return sb.String()
}

// generateOVSNetworkXML generates XML for an OVS-based network
func generateOVSNetworkXML(netCfg config.NetworkConfig) string {
	var sb strings.Builder

	sb.WriteString("<network>\n")
	sb.WriteString(fmt.Sprintf("  <name>%s</name>\n", netCfg.Name))
	sb.WriteString("  <forward mode='bridge'/>\n")
	sb.WriteString(fmt.Sprintf("  <bridge name='%s'/>\n", netCfg.BridgeName))
	sb.WriteString("  <virtualport type='openvswitch'/>\n")
	sb.WriteString("</network>\n")

	return sb.String()
}

// CreateOVSBridge creates an OVS bridge
func CreateOVSBridge(bridgeName string) error {
	// Check if bridge already exists
	checkCmd := exec.Command("ovs-vsctl", "br-exists", bridgeName)
	if err := checkCmd.Run(); err == nil {
		// Bridge exists, skip creation
		fmt.Printf("OVS bridge %s already exists, skipping creation\n", bridgeName)
		return nil
	}

	// Create the bridge
	createCmd := exec.Command("ovs-vsctl", "add-br", bridgeName)
	if output, err := createCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create OVS bridge %s: %w, output: %s", bridgeName, err, string(output))
	}

	fmt.Printf("✓ Created OVS bridge: %s\n", bridgeName)
	return nil
}

// DeleteOVSBridge deletes an OVS bridge
func DeleteOVSBridge(bridgeName string) error {
	// Check if bridge exists
	checkCmd := exec.Command("ovs-vsctl", "br-exists", bridgeName)
	if err := checkCmd.Run(); err != nil {
		// Bridge doesn't exist, nothing to do
		return nil
	}

	// Delete the bridge
	deleteCmd := exec.Command("ovs-vsctl", "del-br", bridgeName)
	if output, err := deleteCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to delete OVS bridge %s: %w, output: %s", bridgeName, err, string(output))
	}

	return nil
}

// CreateHostToDPUNetwork creates a network for host-to-DPU connection
func CreateHostToDPUNetwork(conn *libvirt.Connect, hostName, dpuName string, useOVS bool) error {
	networkName := network.GetHostToDPUNetworkName(hostName, dpuName)
	bridgeName := network.GenerateBridgeName(hostName, dpuName)

	// Check if network already exists
	if NetworkExists(conn, networkName) {
		fmt.Printf("Host-to-DPU network %s already exists, skipping creation\n", networkName)
		return nil
	}

	var xml string
	if useOVS {
		// Create OVS bridge first
		if err := CreateOVSBridge(bridgeName); err != nil {
			return fmt.Errorf("failed to create OVS bridge for host-to-DPU: %w", err)
		}

		// Generate OVS network XML
		xml = fmt.Sprintf(`<network>
  <name>%s</name>
  <forward mode='bridge'/>
  <bridge name='%s'/>
  <virtualport type='openvswitch'/>
</network>`, networkName, bridgeName)
	} else {
		// Generate simple bridge network XML
		xml = fmt.Sprintf(`<network>
  <name>%s</name>
  <bridge name='%s' stp='on' delay='0'/>
</network>`, networkName, bridgeName)
	}

	// Define and start the network
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

// CreateAllNetworks creates all networks defined in the configuration
func CreateAllNetworks(cfg *config.Config, conn *libvirt.Connect) error {
	fmt.Println("=== Creating Networks ===")

	// Create configured networks
	for _, netCfg := range cfg.Networks {
		if err := CreateNetwork(conn, netCfg); err != nil {
			return fmt.Errorf("failed to create network %s: %w", netCfg.Name, err)
		}
	}

	// Create host-to-DPU networks
	pairs := cfg.GetHostDPUPairs()
	for _, pair := range pairs {
		// Check if any network uses OVS to determine bridge type
		useOVS := false
		for _, netCfg := range cfg.Networks {
			if netCfg.UseOVS {
				useOVS = true
				break
			}
		}

		if err := CreateHostToDPUNetwork(conn, pair.Host.Name, pair.DPU.Name, useOVS); err != nil {
			return fmt.Errorf("failed to create host-to-DPU network for %s-%s: %w",
				pair.Host.Name, pair.DPU.Name, err)
		}
	}

	fmt.Println("✓ All networks created successfully")
	return nil
}
