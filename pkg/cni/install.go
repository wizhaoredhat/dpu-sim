package cni

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// InstallCNI installs the specified CNI on a cluster
func (m *CNIManager) InstallCNI(cniType CNIType, clusterName, kubeconfigPath string) error {
	fmt.Printf("Installing %s CNI on cluster %s...\n", cniType, clusterName)

	switch cniType {
	case CNIFlannel:
		return m.installFlannel(kubeconfigPath)
	case CNIOVNKubernetes:
		return m.installOVNKubernetes(kubeconfigPath, clusterName)
	case CNIMultus:
		return m.installMultus(kubeconfigPath)
	case CNIKindnet:
		fmt.Println("Kindnet is the default CNI for Kind clusters, no installation needed")
		return nil
	default:
		return fmt.Errorf("unsupported CNI type: %s", cniType)
	}
}

// installFlannel installs Flannel CNI
func (m *CNIManager) installFlannel(kubeconfigPath string) error {
	fmt.Println("  Installing Flannel CNI...")

	flannelURL := "https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml"

	cmd := exec.Command("kubectl", "apply", "-f", flannelURL, "--kubeconfig", kubeconfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to install Flannel: %w, output: %s", err, string(output))
	}

	fmt.Println("  ✓ Flannel installed")

	// Wait for Flannel pods to be ready
	if err := m.waitForPods(kubeconfigPath, "kube-flannel", "kube-system", 3*time.Minute); err != nil {
		fmt.Printf("Warning: Flannel pods may not be ready: %v\n", err)
	}

	return nil
}

// installOVNKubernetes installs OVN-Kubernetes CNI
func (m *CNIManager) installOVNKubernetes(kubeconfigPath, clusterName string) error {
	fmt.Println("  Installing OVN-Kubernetes CNI...")

	// Get cluster configuration
	clusterCfg := m.config.GetClusterConfig(clusterName)
	if clusterCfg == nil {
		return fmt.Errorf("cluster %s not found in configuration", clusterName)
	}

	podCIDR := clusterCfg.PodCIDR
	if podCIDR == "" {
		podCIDR = "10.244.0.0/16"
	}

	serviceCIDR := clusterCfg.ServiceCIDR
	if serviceCIDR == "" {
		serviceCIDR = "10.96.0.0/12"
	}

	// Download OVN-Kubernetes manifests
	ovnURL := "https://raw.githubusercontent.com/ovn-org/ovn-kubernetes/master/dist/images/ovnkube-db.yaml"

	cmd := exec.Command("kubectl", "apply", "-f", ovnURL, "--kubeconfig", kubeconfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to install OVN-Kubernetes: %w, output: %s", err, string(output))
	}

	fmt.Println("  ✓ OVN-Kubernetes installed")

	// Wait for OVN pods to be ready
	if err := m.waitForPods(kubeconfigPath, "ovnkube", "ovn-kubernetes", 5*time.Minute); err != nil {
		fmt.Printf("Warning: OVN-Kubernetes pods may not be ready: %v\n", err)
	}

	return nil
}

// installMultus installs Multus CNI
func (m *CNIManager) installMultus(kubeconfigPath string) error {
	fmt.Println("  Installing Multus CNI...")

	multusURL := "https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/deployments/multus-daemonset-thick.yml"

	cmd := exec.Command("kubectl", "apply", "-f", multusURL, "--kubeconfig", kubeconfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to install Multus: %w, output: %s", err, string(output))
	}

	fmt.Println("  ✓ Multus installed")

	// Wait for Multus pods to be ready
	if err := m.waitForPods(kubeconfigPath, "kube-multus", "kube-system", 3*time.Minute); err != nil {
		fmt.Printf("Warning: Multus pods may not be ready: %v\n", err)
	}

	return nil
}

// InstallCNIOnVM installs CNI components on a VM-based cluster node
func (m *CNIManager) InstallCNIOnVM(vmIP, vmName string, cniType CNIType, clusterName string) error {
	fmt.Printf("Installing %s CNI on VM %s...\n", cniType, vmName)

	switch cniType {
	case CNIFlannel:
		return m.installFlannelOnVM(vmIP, clusterName)
	case CNIOVNKubernetes:
		return m.installOVNKubernetesOnVM(vmIP, clusterName)
	case CNIMultus:
		return m.installMultusOnVM(vmIP)
	default:
		return fmt.Errorf("unsupported CNI type: %s", cniType)
	}
}

// installFlannelOnVM installs Flannel on a VM
func (m *CNIManager) installFlannelOnVM(vmIP, clusterName string) error {
	script := `
set -e
kubectl apply -f https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml
`

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_, stderr, err := m.sshClient.ExecuteWithContext(ctx, vmIP, script)
	if err != nil {
		return fmt.Errorf("failed to install Flannel: %w, stderr: %s", err, stderr)
	}

	fmt.Println("  ✓ Flannel installed on VM")
	return nil
}

// installOVNKubernetesOnVM installs OVN-Kubernetes on a VM
func (m *CNIManager) installOVNKubernetesOnVM(vmIP, clusterName string) error {
	script := `
set -e
kubectl apply -f https://raw.githubusercontent.com/ovn-org/ovn-kubernetes/master/dist/images/ovnkube-db.yaml
`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	_, stderr, err := m.sshClient.ExecuteWithContext(ctx, vmIP, script)
	if err != nil {
		return fmt.Errorf("failed to install OVN-Kubernetes: %w, stderr: %s", err, stderr)
	}

	fmt.Println("  ✓ OVN-Kubernetes installed on VM")
	return nil
}

// installMultusOnVM installs Multus on a VM
func (m *CNIManager) installMultusOnVM(vmIP string) error {
	script := `
set -e
kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/deployments/multus-daemonset-thick.yml
`

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_, stderr, err := m.sshClient.ExecuteWithContext(ctx, vmIP, script)
	if err != nil {
		return fmt.Errorf("failed to install Multus: %w, stderr: %s", err, stderr)
	}

	fmt.Println("  ✓ Multus installed on VM")
	return nil
}

// waitForPods waits for pods with a specific label to be ready
func (m *CNIManager) waitForPods(kubeconfigPath, labelSelector, namespace string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	fmt.Printf("  Waiting for %s pods to be ready...\n", labelSelector)

	for time.Now().Before(deadline) {
		cmd := exec.Command("kubectl", "wait", "--for=condition=ready", "pod",
			"-l", fmt.Sprintf("app=%s", labelSelector),
			"-n", namespace,
			"--timeout=10s",
			"--kubeconfig", kubeconfigPath)

		if err := cmd.Run(); err == nil {
			fmt.Printf("  ✓ %s pods are ready\n", labelSelector)
			return nil
		}

		<-ticker.C
	}

	return fmt.Errorf("timeout waiting for %s pods", labelSelector)
}
