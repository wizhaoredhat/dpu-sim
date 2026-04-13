package cni

import (
	"fmt"
	"strings"
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
		manifest = rewriteMultusBinDir(manifest, writableCNIBinDir)
		log.Info("Detected bootc/read-only root setup, patching Multus CNI binary path to %s", writableCNIBinDir)
	}

	if err := m.k8sClient.ApplyManifest(manifest); err != nil {
		return fmt.Errorf("failed to install Multus: %w", err)
	}

	log.Info("✓ Multus is installed")

	// Wait for Multus daemonset pods to be ready.
	if err := m.k8sClient.WaitForPodsReady("kube-system", "name=multus", 3*time.Minute); err != nil {
		return fmt.Errorf("multus pods are not ready: %w", err)
	}

	// Recreate CoreDNS after Multus is active so pods pick up stable CNI wiring.
	if !m.config.IsOffloadDPU() {
		if err := m.k8sClient.RolloutRestartDeployment("kube-system", "coredns"); err != nil {
			log.Warn("failed to restart coredns after multus install: %w", err)
		}
		if err := m.k8sClient.WaitForDeploymentAvailable("kube-system", "coredns", 5*time.Minute); err != nil {
			log.Warn("Warning: coredns deployment is not available after multus install: %v", err)
		}
	}

	return nil
}

func rewriteMultusBinDir(manifest []byte, hostPath string) []byte {
	content := string(manifest)
	if strings.Contains(content, "\"binDir\":") {
		return manifest
	}

	hostBinPath := hostPath
	content = strings.Replace(
		content,
		"\"socketDir\": \"/host/run/multus/\"",
		fmt.Sprintf("\"socketDir\": \"/host/run/multus/\",\n        \"binDir\": \"%s\"", hostBinPath),
		1,
	)

	return []byte(content)
}
