package kind

import (
	"fmt"
	"strings"

	"github.com/wizhao/dpu-sim/pkg/config"
)

// CleanupAll performs comprehensive cleanup of all resources and attempts to
// continue cleanup even if errors occurs.
func (m *KindManager) CleanupAll(cfg *config.Config) error {
	errors := make([]string, 0)

	for _, cluster := range cfg.Kubernetes.Clusters {
		if err := m.DeleteCluster(cluster.Name); err != nil {
			errors = append(errors, fmt.Sprintf("Failed to delete cluster %s: %v", cluster.Name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errors, "; "))
	}

	return nil
}
