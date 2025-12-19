package cni

import (
	"fmt"
	"time"
)

// FlannelManifestURL is the URL for the Flannel CNI manifest
const FlannelManifestURL = "https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml"

// installFlannel installs Flannel CNI using the Kubernetes API
func (m *CNIManager) installFlannel(clusterName string) error {
	fmt.Println("  Installing Flannel CNI...")

	if err := m.k8sClient.ApplyManifestFromURL(FlannelManifestURL); err != nil {
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
		fmt.Printf("Warning: failed to patch Flannel config: %v\n", err)
	}

	// Restart Flannel DaemonSet to pick up the new configuration
	if err := m.k8sClient.RolloutRestartDaemonSet("kube-flannel", "kube-flannel-ds"); err != nil {
		fmt.Printf("Warning: failed to restart Flannel daemonset: %v\n", err)
	}

	fmt.Println("✓ Flannel installed")

	// Wait for Flannel pods to be ready
	if err := m.k8sClient.WaitForPodsReady("kube-flannel", "", 3*time.Minute); err != nil {
		fmt.Printf("Warning: Flannel pods may not be ready: %v\n", err)
	}

	return nil
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

	fmt.Printf("✓ Flannel config updated with pod CIDR: %s\n", podCIDR)
	return nil
}
