package cni

import (
	"fmt"
	"time"
)

// MultusManifestURL is the URL for the Multus CNI manifest
const MultusManifestURL = "https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/deployments/multus-daemonset-thick.yml"

// installMultus installs Multus CNI using the Kubernetes API
func (m *CNIManager) installMultus() error {
	fmt.Println("  Installing Multus CNI...")

	if err := m.k8sClient.ApplyManifestFromURL(MultusManifestURL); err != nil {
		return fmt.Errorf("failed to install Multus: %w", err)
	}

	fmt.Println("✓ Multus installed")

	// Wait for Multus pods to be ready
	if err := m.k8sClient.WaitForPodsReady("kube-system", "", 3*time.Minute); err != nil {
		fmt.Printf("Warning: Multus pods may not be ready: %v\n", err)
	}

	return nil
}
