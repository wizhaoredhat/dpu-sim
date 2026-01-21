package vm

import (
	"fmt"
	"net"
	"strings"
	"time"

	"libvirt.org/go/libvirt"

	"github.com/wizhao/dpu-sim/pkg/config"
)

// GetVMMgmtIP retrieves the management network IP address of a VM.
func (m *VMManager) GetVMMgmtIP(vmName string) (string, error) {
	return m.WaitForVMIP(vmName, config.MgmtNetworkName, 10*time.Second)
}

// GetVMK8sIP retrieves the Kubernetes network IP address of a VM.
func (m *VMManager) GetVMK8sIP(vmName string) (string, error) {
	return m.WaitForVMIP(vmName, config.K8sNetworkName, 10*time.Second)
}

// GetVMIP retrieves the IP address of a VM by name and network type.
// networkType should be "mgmt" or "k8s" to specify which network's IP to retrieve.
// The subnet information is retrieved from the config file based on the network type.
func (m *VMManager) GetVMIP(vmName string, networkType string) (string, error) {
	network := m.config.GetNetworkByType(networkType)
	if network == nil {
		return "", fmt.Errorf("network type %q not found in configuration", networkType)
	}

	subnet := network.GetSubnetCIDR()
	if subnet == "" {
		return "", fmt.Errorf("could not determine subnet for network type %q", networkType)
	}

	return m.GetVMIPBySubnet(vmName, subnet)
}

// WaitForVMIP waits for a VM to get an IP address on the specified network type.
// networkType should be "mgmt" or "k8s" to specify which network's IP to wait for.
func (m *VMManager) WaitForVMIP(vmName string, networkType string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		ip, err := m.GetVMIP(vmName, networkType)
		if err == nil && ip != "" {
			return ip, nil
		}
		<-ticker.C
	}

	return "", fmt.Errorf("timeout waiting for IP address for VM %s on network %s", vmName, networkType)
}

// GetVMIPBySubnet retrieves the IP address of a VM that belongs to the specified subnet.
// subnet should be in CIDR notation (e.g., "192.168.120.0/24").
func (m *VMManager) GetVMIPBySubnet(vmName string, subnet string) (string, error) {
	domain, err := m.conn.LookupDomainByName(vmName)
	if err != nil {
		return "", fmt.Errorf("failed to lookup domain %s: %w", vmName, err)
	}
	defer domain.Free()

	// Parse the subnet CIDR
	_, ipNet, err := net.ParseCIDR(subnet)
	if err != nil {
		return "", fmt.Errorf("failed to parse subnet %s: %w", subnet, err)
	}

	// Get domain interfaces
	ifaces, err := domain.ListAllInterfaceAddresses(libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE)
	if err != nil {
		return "", fmt.Errorf("failed to get interfaces for %s: %w", vmName, err)
	}

	// Find IPv4 address that belongs to the specified subnet
	for _, iface := range ifaces {
		for _, addr := range iface.Addrs {
			if addr.Type == libvirt.IP_ADDR_TYPE_IPV4 {
				ip := net.ParseIP(addr.Addr)
				if ip != nil && ipNet.Contains(ip) {
					return addr.Addr, nil
				}
			}
		}
	}

	return "", fmt.Errorf("no IP address found for VM %s in subnet %s", vmName, subnet)
}

// GetVMState retrieves the state of a VM
func (m *VMManager) GetVMState(vmName string) (VMState, error) {
	domain, err := m.conn.LookupDomainByName(vmName)
	if err != nil {
		return VMStateUnknown, fmt.Errorf("failed to lookup domain %s: %w", vmName, err)
	}
	defer domain.Free()

	state, _, err := domain.GetState()
	if err != nil {
		return VMStateUnknown, fmt.Errorf("failed to get state for %s: %w", vmName, err)
	}

	return libvirtStateToVMState(state), nil
}

// libvirtStateToVMState converts libvirt state to our VMState
func libvirtStateToVMState(state libvirt.DomainState) VMState {
	switch state {
	case libvirt.DOMAIN_RUNNING:
		return VMStateRunning
	case libvirt.DOMAIN_BLOCKED:
		return VMStateBlocked
	case libvirt.DOMAIN_PAUSED:
		return VMStatePaused
	case libvirt.DOMAIN_SHUTDOWN:
		return VMStateShutdown
	case libvirt.DOMAIN_SHUTOFF:
		return VMStateShutoff
	case libvirt.DOMAIN_CRASHED:
		return VMStateCrashed
	default:
		return VMStateUnknown
	}
}

// VMExists checks if a VM exists
func (m *VMManager) VMExists(vmName string) bool {
	domain, err := m.conn.LookupDomainByName(vmName)
	if err != nil {
		return false
	}
	defer domain.Free()
	return true
}

// GetVMInterfaceInfo retrieves interface information from a VM
func (m *VMManager) GetVMInterfaceInfo(vmName string) ([]InterfaceInfo, error) {
	domain, err := m.conn.LookupDomainByName(vmName)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup domain %s: %w", vmName, err)
	}
	defer domain.Free()

	ifaces, err := domain.ListAllInterfaceAddresses(libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_AGENT)
	if err != nil {
		return nil, fmt.Errorf("failed to get interfaces for %s: %w", vmName, err)
	}

	result := make([]InterfaceInfo, 0, len(ifaces))
	for _, iface := range ifaces {
		info := InterfaceInfo{
			Name:   iface.Name,
			Hwaddr: iface.Hwaddr,
			Addrs:  make([]string, 0, len(iface.Addrs)),
		}
		for _, addr := range iface.Addrs {
			info.Addrs = append(info.Addrs, addr.Addr)
		}
		result = append(result, info)
	}

	return result, nil
}

// GetVMInfo retrieves comprehensive information about a VM.
// networkType should be "mgmt" or "k8s" to specify which network's IP to retrieve.
func (m *VMManager) GetVMInfo(vmName string, networkType string) (*VMInfo, error) {
	domain, err := m.conn.LookupDomainByName(vmName)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup domain %s: %w", vmName, err)
	}
	defer domain.Free()

	// Get state
	state, _, err := domain.GetState()
	if err != nil {
		return nil, fmt.Errorf("failed to get state: %w", err)
	}

	// Get domain info
	info, err := domain.GetInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get domain info: %w", err)
	}

	// Get IP address (may fail if VM is not running)
	ip, _ := m.GetVMIP(vmName, networkType)

	return &VMInfo{
		Name:      vmName,
		State:     libvirtStateToVMState(libvirt.DomainState(state)),
		IP:        ip,
		VCPUs:     info.NrVirtCpu,
		MemoryMB:  info.Memory / 1024,
		MaxMemory: info.MaxMem / 1024,
	}, nil
}

// String returns a formatted string representation of VMInfo
func (v *VMInfo) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Name: %s\n", v.Name))
	sb.WriteString(fmt.Sprintf("State: %s\n", v.State))
	sb.WriteString(fmt.Sprintf("IP: %s\n", v.IP))
	sb.WriteString(fmt.Sprintf("VCPUs: %d\n", v.VCPUs))
	sb.WriteString(fmt.Sprintf("Memory: %d MB\n", v.MemoryMB))
	return sb.String()
}
