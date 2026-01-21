package vm

import (
	"fmt"
	"strings"
)

// CleanupAll performs comprehensive cleanup of all resources and attempts to
// continue cleanup even if errors occurs.
func (m *VMManager) CleanupAll() error {
	errors := make([]string, 0)

	if err := m.CleanupVMs(); err != nil {
		errors = append(errors, fmt.Sprintf("VM cleanup: %v", err))
	}

	if err := m.CleanupNetworks(); err != nil {
		errors = append(errors, fmt.Sprintf("Network cleanup: %v", err))
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errors, "; "))
	}

	return nil
}
