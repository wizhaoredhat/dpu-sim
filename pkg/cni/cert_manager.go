package cni

import (
	"fmt"
	"time"

	"github.com/wizhao/dpu-sim/pkg/log"
)

const (
	// CertManagerVersion pins cert-manager to a tested version for reproducible installs.
	CertManagerVersion = "v1.16.3"
)

var certManagerManifestURL = fmt.Sprintf(
	"https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml",
	CertManagerVersion,
)

func (m *CNIManager) installCertManager(clusterName string) error {
	if m.k8sClient == nil {
		return fmt.Errorf("kubernetes client is required to install cert-manager addon on cluster %s", clusterName)
	}

	log.Info("Installing cert-manager %s on cluster %s...", CertManagerVersion, clusterName)
	if err := m.k8sClient.ApplyManifestFromURL(certManagerManifestURL); err != nil {
		return fmt.Errorf("failed to apply cert-manager manifest: %w", err)
	}

	m.k8sClient.InvalidateDiscoveryCache()

	deployments := []string{
		"cert-manager",
		"cert-manager-webhook",
		"cert-manager-cainjector",
	}

	for _, deployment := range deployments {
		if err := m.k8sClient.WaitForDeploymentAvailable("cert-manager", deployment, 5*time.Minute); err != nil {
			return fmt.Errorf("cert-manager addon deployment %q is not available: %w", deployment, err)
		}
	}

	log.Info("✓ cert-manager is installed and ready on cluster %s", clusterName)
	return nil
}
