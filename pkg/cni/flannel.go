package cni

import (
	"fmt"
	"strings"
	"time"

	"github.com/wizhao/dpu-sim/pkg/log"
)

// FlannelManifestURL is the URL for the Flannel CNI manifest
const FlannelManifestURL = "https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml"

// installFlannel installs Flannel CNI using the Kubernetes API
func (m *CNIManager) installFlannel(clusterName string) error {
	log.Debug("Installing Flannel CNI on cluster %s...", clusterName)

	manifest, err := downloadManifest(FlannelManifestURL)
	if err != nil {
		return fmt.Errorf("failed to download Flannel manifest: %w", err)
	}

	if m.shouldUseWritableCNIBinDir() {
		manifest = rewriteCNIBinPath(manifest, writableCNIBinDir)
		log.Info("Patching Flannel CNI binary path to %s", writableCNIBinDir)
	}

	if m.config.IsKindMode() {
		manifest = disableFlannelIptablesForwardRules(manifest)
	}

	if err := m.k8sClient.ApplyManifest(manifest); err != nil {
		return fmt.Errorf("failed to install Flannel: %w", err)
	}

	// Get pod CIDR from cluster config
	var podCIDR string
	clusterCfg := m.config.GetClusterConfig(clusterName)
	if clusterCfg == nil {
		return fmt.Errorf("failed to get cluster config: %s", clusterName)
	}
	podCIDR = clusterCfg.PodCIDR

	// Patch the kube-flannel-cfg ConfigMap with the correct pod CIDR
	if err := m.patchFlannelConfig(podCIDR); err != nil {
		log.Warn("Warning: failed to patch Flannel config: %v", err)
	}

	// Restart Flannel DaemonSet to pick up the new configuration
	if err := m.k8sClient.RolloutRestartDaemonSet("kube-flannel", "kube-flannel-ds"); err != nil {
		log.Warn("Warning: failed to restart Flannel daemonset: %v", err)
	}

	log.Info("✓ Flannel is installed on cluster %s", clusterName)

	// Wait for Flannel pods to be ready before installing addons that depend on
	// the default network.
	if err := m.k8sClient.WaitForPodsReady("kube-flannel", "", 3*time.Minute); err != nil {
		return fmt.Errorf("flannel pods are not ready: %w", err)
	}

	return nil
}

// disableFlannelIptablesForwardRules disables Flannel's automatic FORWARD
// chain rule management in Kind mode.
//
// Why: Kind nodes run inside containers and FORWARD chain policy/rules are
// controlled by the container runtime/host. Letting Flannel mutate those rules
// can introduce non-deterministic behavior across hosts (especially nftables
// systems), causing flaky pod networking during addon rollout.
func disableFlannelIptablesForwardRules(manifest []byte) []byte {
	content := string(manifest)
	if strings.Contains(content, "--iptables-forward-rules=false") {
		return manifest
	}

	content = strings.Replace(content, "        - --kube-subnet-mgr", "        - --kube-subnet-mgr\n        - --iptables-forward-rules=false", 1)
	return []byte(content)
}

// patchFlannelConfig patches the kube-flannel-cfg ConfigMap with the correct pod CIDR
func (m *CNIManager) patchFlannelConfig(podCIDR string) error {
	configMap, err := m.k8sClient.GetConfigMap("kube-flannel", "kube-flannel-cfg")
	if err != nil {
		return fmt.Errorf("failed to get Flannel configmap: %w", err)
	}

	// Update the net-conf.json with the correct pod CIDR
	netConf := fmt.Sprintf(`{"Network": "%s", "Backend": {"Type": "vxlan"}}`, podCIDR)
	configMap.Data["net-conf.json"] = netConf

	_, err = m.k8sClient.UpdateConfigMap(configMap)
	if err != nil {
		return fmt.Errorf("failed to update Flannel configmap: %w", err)
	}

	log.Debug("✓ Flannel config is updated with pod CIDR: %s", podCIDR)
	return nil
}
