package vm

import (
	"fmt"
	"strings"

	"github.com/wizhao/dpu-sim/pkg/log"
	"libvirt.org/go/libvirt"
)

// StartVM starts a VM
func (m *VMManager) StartVM(vmName string) error {
	domain, err := m.conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to lookup domain %s: %w", vmName, err)
	}
	defer domain.Free()

	active, err := domain.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check if domain is active: %w", err)
	}

	if active {
		return fmt.Errorf("VM %s is already running", vmName)
	}

	if err := domain.Create(); err != nil {
		return fmt.Errorf("failed to start VM %s: %w", vmName, err)
	}

	return nil
}

// StopVM shuts down a VM
func (m *VMManager) StopVM(vmName string) error {
	domain, err := m.conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to lookup domain %s: %w", vmName, err)
	}
	defer domain.Free()

	active, err := domain.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check if domain is active: %w", err)
	}

	if !active {
		return fmt.Errorf("VM %s is already stopped", vmName)
	}

	if err := domain.Shutdown(); err != nil {
		return fmt.Errorf("failed to shutdown VM %s: %w", vmName, err)
	}

	return nil
}

// DestroyVM forcefully stops a VM
func (m *VMManager) DestroyVM(vmName string) error {
	domain, err := m.conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to lookup domain %s: %w", vmName, err)
	}
	defer domain.Free()

	active, err := domain.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check if domain is active: %w", err)
	}

	if active {
		// Forcefully stop the domain
		if err := domain.Destroy(); err != nil {
			return fmt.Errorf("failed to destroy VM %s: %w", vmName, err)
		}
	}

	return nil
}

// RebootVM reboots a VM
func (m *VMManager) RebootVM(vmName string) error {
	domain, err := m.conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to lookup domain %s: %w", vmName, err)
	}
	defer domain.Free()

	active, err := domain.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check if domain is active: %w", err)
	}

	if !active {
		return fmt.Errorf("VM %s is not running", vmName)
	}

	if err := domain.Reboot(libvirt.DOMAIN_REBOOT_DEFAULT); err != nil {
		return fmt.Errorf("failed to reboot VM %s: %w", vmName, err)
	}

	return nil
}

// DeleteVM undefines (deletes) a VM and its associated storage
func (m *VMManager) DeleteVM(vmName string) error {
	domain, err := m.conn.LookupDomainByName(vmName)
	if err != nil {
		// VM doesn't exist, nothing to do
		return nil
	}
	defer domain.Free()

	// Stop if running
	active, err := domain.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check if domain is active: %w", err)
	}

	if active {
		if err := domain.Destroy(); err != nil {
			return fmt.Errorf("failed to destroy VM before deletion: %w", err)
		}
	}

	// Undefine the domain with all storage
	if err := domain.UndefineFlags(libvirt.DOMAIN_UNDEFINE_MANAGED_SAVE | libvirt.DOMAIN_UNDEFINE_SNAPSHOTS_METADATA | libvirt.DOMAIN_UNDEFINE_NVRAM); err != nil {
		return fmt.Errorf("failed to undefine VM %s: %w", vmName, err)
	}

	if err := DeleteVMDisk(vmName); err != nil {
		return fmt.Errorf("failed to delete VM disk %s: %w", vmName, err)
	}

	if err := DeleteCloudInitISO(vmName); err != nil {
		return fmt.Errorf("failed to delete cloud-init ISO %s: %w", vmName, err)
	}

	return nil
}

// CleanupVMs removes all VMs defined in the configuration
func (m *VMManager) CleanupVMs() error {
	log.Info("=== Cleaning up VMs ===")

	errors := make([]string, 0)
	for _, vmCfg := range m.config.VMs {
		vmName := vmCfg.Name
		log.Debug("Cleaning up VM: %s...", vmName)

		if err := m.DeleteVM(vmName); err != nil {
			log.Error("✗ Failed to remove VM %s: %v", vmName, err)
			errors = append(errors, fmt.Sprintf("failed to remove VM %s: %v", vmName, err))
			continue
		}

		log.Info("✓ Cleaned up VM: %s", vmName)
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup VMs errors: %s", strings.Join(errors, "; "))
	}

	return nil
}

// SetAutostart configures a VM to start automatically on host boot
func (m *VMManager) SetAutostart(vmName string, autostart bool) error {
	domain, err := m.conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to lookup domain %s: %w", vmName, err)
	}
	defer domain.Free()

	if err := domain.SetAutostart(autostart); err != nil {
		return fmt.Errorf("failed to set autostart for VM %s: %w", vmName, err)
	}

	return nil
}

// ListAllVMs returns a list of all VMs
func (m *VMManager) ListAllVMs() ([]string, error) {
	domains, err := m.conn.ListAllDomains(libvirt.CONNECT_LIST_DOMAINS_ACTIVE | libvirt.CONNECT_LIST_DOMAINS_INACTIVE)
	if err != nil {
		return nil, fmt.Errorf("failed to list domains: %w", err)
	}

	names := make([]string, 0, len(domains))
	for _, domain := range domains {
		name, err := domain.GetName()
		if err != nil {
			domain.Free()
			continue
		}
		names = append(names, name)
		domain.Free()
	}

	return names, nil
}
