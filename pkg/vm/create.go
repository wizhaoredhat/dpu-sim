package vm

import (
	"fmt"
	"strings"

	"libvirt.org/go/libvirt"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/network"
)

// CreateVM creates a complete VM including disk, cloud-init ISO, and domain
func CreateVM(conn *libvirt.Connect, cfg *config.Config, vmCfg config.VMConfig) error {
	fmt.Printf("=== Creating VM: %s ===\n", vmCfg.Name)

	// Check if VM already exists
	if VMExists(conn, vmCfg.Name) {
		fmt.Printf("VM %s already exists\n", vmCfg.Name)
		return nil
	}

	// Download cloud image if needed
	imagePath := GetImagePath(cfg.OperatingSystem)
	if err := DownloadCloudImage(cfg.OperatingSystem.ImageURL, imagePath); err != nil {
		return fmt.Errorf("failed to download cloud image: %w", err)
	}

	// Create VM disk based on cloud image
	diskPath, err := CreateVMDisk(vmCfg.Name, vmCfg.DiskSize, imagePath)
	if err != nil {
		return fmt.Errorf("failed to create VM disk: %w", err)
	}

	// Create cloud-init ISO
	cloudInitPath, err := CreateCloudInitISO(vmCfg.Name, cfg.SSH, vmCfg)
	if err != nil {
		return fmt.Errorf("failed to create cloud-init ISO: %w", err)
	}

	// Generate libvirt domain XML
	xml := GenerateVMXML(vmCfg, diskPath, cloudInitPath, cfg)

	// Define the domain
	domain, err := conn.DomainDefineXML(xml)
	if err != nil {
		return fmt.Errorf("failed to define domain: %w", err)
	}
	defer domain.Free()

	// Set autostart
	if err := domain.SetAutostart(true); err != nil {
		return fmt.Errorf("failed to set autostart: %w", err)
	}

	// Start the VM
	if err := domain.Create(); err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	fmt.Printf("✓ Created and started VM: %s\n", vmCfg.Name)
	return nil
}

// GenerateVMXML generates libvirt domain XML for a VM
func GenerateVMXML(vmCfg config.VMConfig, diskPath, cloudInitPath string, cfg *config.Config) string {
	var sb strings.Builder

	sb.WriteString("<domain type='kvm'>\n")
	sb.WriteString(fmt.Sprintf("  <name>%s</name>\n", vmCfg.Name))
	sb.WriteString(fmt.Sprintf("  <memory unit='MiB'>%d</memory>\n", vmCfg.Memory))
	sb.WriteString(fmt.Sprintf("  <vcpu>%d</vcpu>\n", vmCfg.VCPUs))

	// OS configuration
	sb.WriteString("  <os>\n")
	sb.WriteString("    <type arch='x86_64' machine='pc'>hvm</type>\n")
	sb.WriteString("    <boot dev='hd'/>\n")
	sb.WriteString("  </os>\n")

	// Features
	sb.WriteString("  <features>\n")
	sb.WriteString("    <acpi/>\n")
	sb.WriteString("    <apic/>\n")
	sb.WriteString("  </features>\n")

	// CPU mode
	sb.WriteString("  <cpu mode='host-passthrough'/>\n")

	// Clock
	sb.WriteString("  <clock offset='utc'/>\n")

	// Devices
	sb.WriteString("  <devices>\n")

	// Emulator
	sb.WriteString("    <emulator>/usr/bin/qemu-system-x86_64</emulator>\n")

	// Main disk
	sb.WriteString("    <disk type='file' device='disk'>\n")
	sb.WriteString("      <driver name='qemu' type='qcow2'/>\n")
	sb.WriteString(fmt.Sprintf("      <source file='%s'/>\n", diskPath))
	sb.WriteString("      <target dev='vda' bus='virtio'/>\n")
	sb.WriteString("    </disk>\n")

	// Cloud-init ISO
	sb.WriteString("    <disk type='file' device='cdrom'>\n")
	sb.WriteString("      <driver name='qemu' type='raw'/>\n")
	sb.WriteString(fmt.Sprintf("      <source file='%s'/>\n", cloudInitPath))
	sb.WriteString("      <target dev='sda' bus='sata'/>\n")
	sb.WriteString("      <readonly/>\n")
	sb.WriteString("    </disk>\n")

	// Network interfaces
	for i, netName := range vmCfg.Networks {
		sb.WriteString(fmt.Sprintf("    <interface type='network'>\n"))
		sb.WriteString(fmt.Sprintf("      <source network='%s'/>\n", netName))
		sb.WriteString("      <model type='virtio'/>\n")

		// Generate MAC address based on VM name and interface index
		mac := generateMACAddress(vmCfg.Name, i)
		sb.WriteString(fmt.Sprintf("      <mac address='%s'/>\n", mac))
		sb.WriteString("    </interface>\n")
	}

	// Add host-to-DPU network interface if this is a DPU
	if vmCfg.Type == "dpu" {
		// Find the host this DPU is paired with
		pairs := cfg.GetHostDPUPairs()
		for _, pair := range pairs {
			if pair.DPU.Name == vmCfg.Name {
				networkName := network.GetHostToDPUNetworkName(pair.Host.Name, pair.DPU.Name)
				sb.WriteString("    <interface type='network'>\n")
				sb.WriteString(fmt.Sprintf("      <source network='%s'/>\n", networkName))
				sb.WriteString("      <model type='virtio'/>\n")
				mac := generateMACAddress(vmCfg.Name, len(vmCfg.Networks))
				sb.WriteString(fmt.Sprintf("      <mac address='%s'/>\n", mac))
				sb.WriteString("    </interface>\n")
				break
			}
		}
	}

	// Add host-to-DPU network interface if this is a host
	if vmCfg.Type == "host" {
		// Find all DPUs this host is paired with
		pairs := cfg.GetHostDPUPairs()
		interfaceIdx := len(vmCfg.Networks)
		for _, pair := range pairs {
			if pair.Host.Name == vmCfg.Name {
				networkName := network.GetHostToDPUNetworkName(pair.Host.Name, pair.DPU.Name)
				sb.WriteString("    <interface type='network'>\n")
				sb.WriteString(fmt.Sprintf("      <source network='%s'/>\n", networkName))
				sb.WriteString("      <model type='virtio'/>\n")
				mac := generateMACAddress(vmCfg.Name, interfaceIdx)
				sb.WriteString(fmt.Sprintf("      <mac address='%s'/>\n", mac))
				sb.WriteString("    </interface>\n")
				interfaceIdx++
			}
		}
	}

	// Serial console
	sb.WriteString("    <serial type='pty'>\n")
	sb.WriteString("      <target port='0'/>\n")
	sb.WriteString("    </serial>\n")

	// Console
	sb.WriteString("    <console type='pty'>\n")
	sb.WriteString("      <target type='serial' port='0'/>\n")
	sb.WriteString("    </console>\n")

	// Graphics (VNC)
	sb.WriteString("    <graphics type='vnc' port='-1' autoport='yes'/>\n")

	// Video
	sb.WriteString("    <video>\n")
	sb.WriteString("      <model type='cirrus'/>\n")
	sb.WriteString("    </video>\n")

	// Channel for QEMU guest agent
	sb.WriteString("    <channel type='unix'>\n")
	sb.WriteString("      <target type='virtio' name='org.qemu.guest_agent.0'/>\n")
	sb.WriteString("    </channel>\n")

	sb.WriteString("  </devices>\n")
	sb.WriteString("</domain>\n")

	return sb.String()
}

// generateMACAddress generates a MAC address based on VM name and interface index
func generateMACAddress(vmName string, interfaceIndex int) string {
	// Use a simple hash-based approach
	// Start with 52:54:00 prefix (QEMU default range)
	hash := 0
	for _, c := range vmName {
		hash = hash*31 + int(c)
	}
	hash = hash + interfaceIndex

	b3 := byte((hash >> 16) & 0xFF)
	b4 := byte((hash >> 8) & 0xFF)
	b5 := byte(hash & 0xFF)

	return fmt.Sprintf("52:54:00:%02x:%02x:%02x", b3, b4, b5)
}

// CreateAllVMs creates all VMs defined in the configuration
func CreateAllVMs(cfg *config.Config, conn *libvirt.Connect) error {
	fmt.Println("=== Creating All VMs ===")

	for _, vmCfg := range cfg.VMs {
		if err := CreateVM(conn, cfg, vmCfg); err != nil {
			return fmt.Errorf("failed to create VM %s: %w", vmCfg.Name, err)
		}
	}

	fmt.Println("✓ All VMs created successfully")
	return nil
}
