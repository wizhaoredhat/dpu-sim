package vm

import (
	"fmt"
	"strings"

	"libvirt.org/go/libvirt"

	"github.com/wizhao/dpu-sim/pkg/config"
)

// CleanupAll performs comprehensive cleanup of all resources and attempts to
// continue cleanup even if errors occurs.
func CleanupAll(cfg *config.Config, conn *libvirt.Connect) error {
	errors := make([]string, 0)

	if err := CleanupVMs(cfg, conn); err != nil {
		errors = append(errors, fmt.Sprintf("VM cleanup: %v", err))
	}

	if err := CleanupNetworks(cfg, conn); err != nil {
		errors = append(errors, fmt.Sprintf("Network cleanup: %v", err))
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errors, "; "))
	}

	return nil
}
