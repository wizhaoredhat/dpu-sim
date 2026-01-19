package k8s

import (
	"fmt"
	"strings"

	"github.com/wizhao/dpu-sim/pkg/config"
)

// CleanupAll performs comprehensive cleanup of all resources and attempts to
// continue cleanup even if errors occurs.
func CleanupAll(cfg *config.Config) error {
	errors := make([]string, 0)

	kubeconfigDir := cfg.Kubernetes.KubeconfigDir
	if err := CleanupKubeconfig(kubeconfigDir); err != nil {
		errors = append(errors, fmt.Sprintf("Kubeconfig cleanup: %v", err))
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errors, "; "))
	}

	return nil
}
