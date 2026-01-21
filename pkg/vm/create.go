package vm

import (
	"fmt"
	"strings"

	"github.com/wizhao/dpu-sim/pkg/config"
)

// CreateVM creates a complete VM including disk, cloud-init ISO, and domain
func (m *VMManager) CreateVM(vmCfg config.VMConfig) error {
	fmt.Printf("=== Creating VM: %s ===\n", vmCfg.Name)

	if m.VMExists(vmCfg.Name) {
		return fmt.Errorf("VM %s already exists", vmCfg.Name)
	}

	imagePath := GetImagePath(m.config.OperatingSystem)

	if err := DownloadCloudImage(m.config.OperatingSystem.ImageURL, imagePath); err != nil {
		return fmt.Errorf("failed to download cloud image: %w", err)
	}

	diskPath, err := CreateVMDisk(vmCfg.Name, vmCfg.DiskSize, imagePath)
	if err != nil {
		return fmt.Errorf("failed to create VM disk: %w", err)
	}

	cloudInitPath, err := CreateCloudInitISO(vmCfg.Name, m.config.SSH, vmCfg)
	if err != nil {
		return fmt.Errorf("failed to create cloud-init ISO: %w", err)
	}

	// Generate libvirt domain XML
	xml := m.GenerateVMXML(vmCfg, diskPath, cloudInitPath)

	domain, err := m.conn.DomainDefineXML(xml)
	if err != nil {
		return fmt.Errorf("failed to define domain: %w", err)
	}
	defer domain.Free()

	if err := domain.SetAutostart(true); err != nil {
		return fmt.Errorf("failed to set autostart: %w", err)
	}

	if err := domain.Create(); err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	fmt.Printf("✓ Created and started VM: %s\n", vmCfg.Name)
	return nil
}

func (m *VMManager) generateNetworkInterfaces(vmCfg config.VMConfig) string {
	var sb strings.Builder

	for _, network := range m.config.Networks {
		// AttachTo determines which type of VM the network should be attached to.
		if network.AttachTo != "any" && network.AttachTo != vmCfg.Type {
			continue
		}

		sb.WriteString("    <interface type='network'>\n")
		if network.Type == config.K8sNetworkName && vmCfg.K8sNodeMAC != "" {
			sb.WriteString(fmt.Sprintf("      <mac address='%s'/>\n", vmCfg.K8sNodeMAC))
		}
		sb.WriteString(fmt.Sprintf("      <source network='%s'/>\n", network.Name))
		if network.UseOVS {
			sb.WriteString("      <virtualport type='openvswitch'/>\n")
		}
		sb.WriteString(fmt.Sprintf("      <model type='%s'/>\n", network.NICModel))
		sb.WriteString("    </interface>\n")
	}

	return sb.String()
}

// GenerateVMXML generates libvirt domain XML for a VM
func (m *VMManager) GenerateVMXML(vmCfg config.VMConfig, diskPath, cloudInitPath string) string {
	var sb strings.Builder

	sb.WriteString("<domain type='kvm'>\n")
	sb.WriteString(fmt.Sprintf("  <name>%s</name>\n", vmCfg.Name))
	sb.WriteString(fmt.Sprintf("  <memory unit='MiB'>%d</memory>\n", vmCfg.Memory))
	sb.WriteString(fmt.Sprintf("  <vcpu>%d</vcpu>\n", vmCfg.VCPUs))

	sb.WriteString("  <os>\n")
	sb.WriteString("    <type arch='x86_64' machine='q35'>hvm</type>\n")
	sb.WriteString("    <boot dev='hd'/>\n")
	sb.WriteString("  </os>\n")

	sb.WriteString("  <features>\n")
	sb.WriteString("    <acpi/>\n")
	sb.WriteString("    <apic/>\n")
	sb.WriteString("    <ioapic driver='qemu'/>\n")
	sb.WriteString("  </features>\n")

	sb.WriteString("  <cpu mode='host-passthrough'/>\n")

	sb.WriteString("  <iommu model='intel'>\n")
	sb.WriteString("    <driver intremap='on' caching_mode='on' iotlb='on'/>\n")
	sb.WriteString("  </iommu>\n")

	sb.WriteString("  <clock offset='utc'/>\n")

	sb.WriteString("  <on_poweroff>destroy</on_poweroff>\n")
	sb.WriteString("  <on_reboot>restart</on_reboot>\n")
	sb.WriteString("  <on_crash>destroy</on_crash>\n")

	sb.WriteString("  <devices>\n")
	sb.WriteString("    <emulator>/usr/libexec/qemu-kvm</emulator>\n")

	sb.WriteString("    <disk type='file' device='disk'>\n")
	sb.WriteString("      <driver name='qemu' type='qcow2'/>\n")
	sb.WriteString(fmt.Sprintf("      <source file='%s'/>\n", diskPath))
	sb.WriteString("      <target dev='vda' bus='virtio'/>\n")
	sb.WriteString("    </disk>\n")

	sb.WriteString("    <disk type='file' device='cdrom'>\n")
	sb.WriteString("      <driver name='qemu' type='raw'/>\n")
	sb.WriteString(fmt.Sprintf("      <source file='%s'/>\n", cloudInitPath))
	sb.WriteString("      <target dev='sda' bus='sata'/>\n")
	sb.WriteString("      <readonly/>\n")
	sb.WriteString("    </disk>\n")

	// Generate network interfaces from looking at the Networks configuration section and seeing if the
	// VM is attached to any of the networks. If so, generate the network interface XML for each of those
	// networks.
	sb.WriteString(m.generateNetworkInterfaces(vmCfg))

	mappings := m.config.GetHostDPUMappings()

	// Add implicit host-to-DPU network interface if this is a host
	if vmCfg.Type == "host" {
		for _, mapping := range mappings {
			if mapping.Host.Name == vmCfg.Name {
				for _, conn := range mapping.Connections {
					sb.WriteString("    <interface type='network'>\n")
					sb.WriteString(fmt.Sprintf("      <source network='%s'/>\n", conn.Link.NetworkName))
					sb.WriteString("      <virtualport type='openvswitch'/>\n")
					sb.WriteString("      <model type='igb'/>\n")
					sb.WriteString("    </interface>\n")
				}
				// Host found, break out of the loop
				break
			}
		}
	}

	if vmCfg.Type == "dpu" {
		for _, mapping := range mappings {
			for _, conn := range mapping.Connections {
				if conn.DPU.Name == vmCfg.Name {
					sb.WriteString("    <interface type='network'>\n")
					sb.WriteString(fmt.Sprintf("      <source network='%s'/>\n", conn.Link.NetworkName))
					sb.WriteString("      <virtualport type='openvswitch'/>\n")
					sb.WriteString("      <model type='igb'/>\n")
					sb.WriteString("    </interface>\n")
					// DPU found, break out of the loop
					break
				}
			}
		}
	}

	sb.WriteString("    <console type='pty'>\n")
	sb.WriteString("      <target type='serial' port='0'/>\n")
	sb.WriteString("    </console>\n")

	sb.WriteString("    <graphics type='vnc' port='-1' autoport='yes'/>\n")

	sb.WriteString("  </devices>\n")
	sb.WriteString("</domain>\n")

	return sb.String()
}

// CreateAllVMs creates all VMs defined in the configuration
func (m *VMManager) CreateAllVMs() error {
	fmt.Println("=== Creating All VMs ===")

	for _, vmCfg := range m.config.VMs {
		if err := m.CreateVM(vmCfg); err != nil {
			return fmt.Errorf("failed to create VM %s: %w", vmCfg.Name, err)
		}
	}

	fmt.Println("✓ All VMs created successfully")
	return nil
}
