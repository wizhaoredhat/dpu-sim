package cni

import (
	"fmt"
	"time"

	"github.com/wizhao/dpu-sim/pkg/log"
)

// MultusManifestURL is the URL for the Multus CNI manifest
const MultusManifestURL = "https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/deployments/multus-daemonset-thick.yml"

// installMultus installs Multus CNI using the Kubernetes API
func (m *CNIManager) installMultus() error {
	log.Debug("Installing Multus CNI...")

	manifest, err := downloadManifest(MultusManifestURL)
	if err != nil {
		return fmt.Errorf("failed to download Multus manifest: %w", err)
	}

	if m.shouldUseWritableCNIBinDir() {
		manifest = rewriteCNIBinPath(manifest, writableCNIBinDir)
		log.Info("Detected bootc/read-only root setup, patching Multus CNI binary path to %s", writableCNIBinDir)
	}

	if err := m.k8sClient.ApplyManifest(manifest); err != nil {
		return fmt.Errorf("failed to install Multus: %w", err)
	}

	log.Info("✓ Multus is installed")

	// Wait for Multus pods to be ready
	if err := m.k8sClient.WaitForPodsReady("kube-system", "", 3*time.Minute); err != nil {
		log.Warn("Warning: Multus pods may not be ready: %v", err)
	}

	return nil
}
