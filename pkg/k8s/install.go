// Package k8s provides functions to install Kubernetes on a machine
package k8s

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// InstallKubernetes installs Kubernetes on a VM
func (m *K8sMachineManager) InstallKubernetes(machineIP, machineName, k8sVersion string) error {
	fmt.Printf("Installing Kubernetes on %s (%s)...\n", machineName, machineIP)

	if err := m.sshClient.WaitForSSH(machineIP, 5*time.Minute); err != nil {
		return fmt.Errorf("failed to wait for SSH: %w", err)
	}

	if err := m.disableSwap(machineIP); err != nil {
		return fmt.Errorf("failed to disable swap for Kubernetes: %w", err)
	}

	if err := m.setHostname(machineIP, machineName); err != nil {
		return fmt.Errorf("failed to set hostname for Kubernetes: %w", err)
	}

	if err := m.configureKernelModules(machineIP); err != nil {
		return fmt.Errorf("failed to configure kernel modules for Kubernetes: %w", err)
	}

	if err := m.installCRIO(machineIP, k8sVersion); err != nil {
		return fmt.Errorf("failed to install CRI-O container runtime: %w", err)
	}

	if err := m.installOpenVSwitch(machineIP); err != nil {
		return fmt.Errorf("failed to install Open vSwitch: %w", err)
	}

	if err := m.addKubernetesRepository(machineIP, k8sVersion); err != nil {
		return fmt.Errorf("failed to add Kubernetes repository: %w", err)
	}

	if err := m.installKubeTools(machineIP); err != nil {
		return fmt.Errorf("failed to install Kubernetes Tools: %w", err)
	}

	if err := m.disableFirewall(machineIP); err != nil {
		return fmt.Errorf("failed to disable firewall: %w", err)
	}

	if err := m.verifyKubernetesInstallation(machineIP); err != nil {
		return fmt.Errorf("failed to verify Kubernetes installation: %w", err)
	}

	fmt.Printf("✓ Kubernetes %s installed on %s\n", k8sVersion, machineName)
	return nil
}

// Disable swap on the machine
//
// From https://github.com/cri-o/packaging/blob/main/README.md
func (m *K8sMachineManager) disableSwap(machineIP string) error {
	fmt.Printf("Disabling swap on %s...\n", machineIP)

	script := `
set -e
sudo swapoff -a
sudo sed -i '/ swap / s/^/#/' /etc/fstab
`
	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, script, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to disable swap: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	fmt.Printf("✓ Swap disabled\n")
	return nil
}

// Set hostname on the machine
func (m *K8sMachineManager) setHostname(machineIP, hostname string) error {
	fmt.Printf("Setting hostname to %s on %s...\n", hostname, machineIP)

	script := fmt.Sprintf("sudo hostnamectl set-hostname %s", hostname)

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, script, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to set hostname: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	fmt.Printf("✓ Hostname set to %s\n", hostname)
	return nil
}

// Configure kernel modules on the machine
//
// From https://kubernetes.io/docs/setup/production-environment/container-runtimes/#configuring-the-container-runtime
func (m *K8sMachineManager) configureKernelModules(machineIP string) error {
	fmt.Printf("Configuring kernel modules on %s...\n", machineIP)

	script := `
set -e
sudo tee /etc/modules-load.d/k8s.conf > /dev/null <<EOF
overlay
br_netfilter
EOF

sudo modprobe overlay
sudo modprobe br_netfilter

# Enable IPv4 packets to be routed between interfaces
sudo tee /etc/sysctl.d/k8s.conf > /dev/null <<EOF
net.bridge.bridge-nf-call-iptables = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward = 1
EOF

# Apply sysctl params without reboot
sudo sysctl --system > /dev/null 2>&1
`

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, script, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to configure kernel modules: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	fmt.Printf("✓ Kernel modules configured\n")
	return nil
}

// Install containerd on the machine
//
// From https://kubernetes.io/docs/setup/production-environment/container-runtimes/#installing-cri-o
func (m *K8sMachineManager) installCRIO(machineIP, k8sVersion string) error {
	fmt.Printf("Installing CRIO on %s...\n", machineIP)

	// Specific for Fedora based linux distributions
	// TODO: Add support for other linux distributions
	script := fmt.Sprintf(`
set -e

# Add CRI-O repository
sudo tee /etc/yum.repos.d/cri-o.repo > /dev/null <<EOF
[cri-o]
name=CRI-O
baseurl=https://pkgs.k8s.io/addons:/cri-o:/stable:/v%[1]s/rpm/
enabled=1
gpgcheck=1
gpgkey=https://pkgs.k8s.io/addons:/cri-o:/stable:/v%[1]s/rpm/repodata/repomd.xml.key
EOF

# Install CRI-O and dependencies
sudo dnf install -y cri-o iproute-tc > /dev/null 2>&1 && \
sudo systemctl enable crio > /dev/null 2>&1 && \
sudo systemctl start crio
`, k8sVersion)

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, script, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to install CRI-O: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	fmt.Printf("✓ CRI-O installed\n")
	return nil
}

func (m *K8sMachineManager) installOpenVSwitch(machineIP string) error {
	fmt.Printf("Installing Open vSwitch on %s...\n", machineIP)

	script := `
set -e
sudo dnf install -y NetworkManager-ovs > /dev/null 2>&1 && \
sudo dnf install -y openvswitch > /dev/null 2>&1 && \
sudo systemctl enable openvswitch > /dev/null 2>&1 && \
sudo systemctl restart NetworkManager > /dev/null 2>&1 && \
sudo systemctl start openvswitch
`

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, script, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to install Open vSwitch: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	fmt.Printf("✓ Open vSwitch installed\n")
	return nil
}

// Add Kubernetes repository on the machine
//
// From https://kubernetes.io/docs/setup/production-environment/container-runtimes/#installing-cri-o
func (m *K8sMachineManager) addKubernetesRepository(machineIP, k8sVersion string) error {
	fmt.Printf("Adding Kubernetes repository on %s...\n", machineIP)

	script := fmt.Sprintf(`
set -e
sudo tee /etc/yum.repos.d/kubernetes.repo > /dev/null <<EOF
[kubernetes]
name=Kubernetes
baseurl=https://pkgs.k8s.io/core:/stable:/v%[1]s/rpm/
enabled=1
gpgcheck=1
gpgkey=https://pkgs.k8s.io/core:/stable:/v%[1]s/rpm/repodata/repomd.xml.key
exclude=kubelet kubeadm kubectl cri-tools kubernetes-cni
EOF
`, k8sVersion)

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, script, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to add Kubernetes repository: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	fmt.Printf("✓ Kubernetes repository added\n")
	return nil
}

// Install kubeadm, kubelet, kubectl (kubernetes tools) on the machine
//
// From https://kubernetes.io/docs/setup/production-environment/tools/kubeadm/install-kubeadm/#installing-kubeadm-kubelet-and-kubectl
// And https://kubernetes.io/docs/setup/production-environment/container-runtimes/#installing-cri-o
func (m *K8sMachineManager) installKubeTools(machineIP string) error {
	fmt.Printf("Installing Kubelet, Kubeadm, Kubectl on %s...\n", machineIP)

	script := `
set -e
sudo dnf install -y kubelet kubeadm kubectl --setopt=disable_excludes=kubernetes > /dev/null 2>&1 && \
sudo systemctl enable kubelet > /dev/null 2>&1
`

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, script, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to install Kubelet: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	fmt.Printf("✓ Kubelet installed\n")
	return nil
}

// Disable firewall on the machine
func (m *K8sMachineManager) disableFirewall(machineIP string) error {
	fmt.Printf("Disabling firewall on %s...\n", machineIP)

	script := `
set -e
sudo systemctl disable --now firewalld
sudo dnf remove -y firewalld
`

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, script, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to configure firewall: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	fmt.Printf("✓ Firewall configured\n")
	return nil
}

// Verify Kubernetes installation on the machine
func (m *K8sMachineManager) verifyKubernetesInstallation(machineIP string) error {
	fmt.Printf("Verifying Kubernetes installation on %s...\n", machineIP)

	script := `
set -e
sudo kubeadm version -o short 2>/dev/null
`

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, script, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to verify Kubernetes installation: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}
	fmt.Printf("✓ Kubeadm version: %s\n", stdout)

	script = `
set -e
sudo systemctl is-active crio
`

	stdout, stderr, err = m.sshClient.ExecuteWithTimeout(machineIP, script, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to verify CRI-O installation: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}
	fmt.Printf("✓ CRI-O is active\n")

	script = `
set -e
sudo ovs-vsctl --version | head -n 1
`

	stdout, stderr, err = m.sshClient.ExecuteWithTimeout(machineIP, script, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to verify Open vSwitch installation: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	fmt.Printf("✓ Open vSwitch version: %s\n", stdout)
	return nil
}

// InitializeControlPlane initializes a Kubernetes control plane node
func (m *K8sMachineManager) InitializeControlPlane(vmIP, vmName, clusterName string) error {
	fmt.Printf("Initializing control plane on %s...\n", vmName)

	// Get cluster configuration
	clusterCfg := m.config.GetClusterConfig(clusterName)
	if clusterCfg == nil {
		return fmt.Errorf("cluster %s not found in configuration", clusterName)
	}

	podCIDR := clusterCfg.PodCIDR
	if podCIDR == "" {
		podCIDR = "10.244.0.0/16" // Default Flannel CIDR
	}

	serviceCIDR := clusterCfg.ServiceCIDR
	if serviceCIDR == "" {
		serviceCIDR = "10.96.0.0/12" // Default service CIDR
	}

	script := fmt.Sprintf(`
set -e
sudo kubeadm init \
  --pod-network-cidr=%s \
  --service-cidr=%s \
  --apiserver-advertise-address=%s
`, podCIDR, serviceCIDR, vmIP)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	stdout, stderr, err := m.sshClient.ExecuteWithContext(ctx, vmIP, script)
	if err != nil {
		return fmt.Errorf("control plane initialization failed: %w, stderr: %s", err, stderr)
	}

	// Extract join command from output
	joinCmd := extractJoinCommand(stdout)
	if joinCmd == "" {
		fmt.Println("Warning: could not extract join command from kubeadm output")
	}

	fmt.Printf("✓ Control plane initialized on %s\n", vmName)
	return nil
}

// JoinNode joins a worker node to the cluster
func (m *K8sMachineManager) JoinNode(vmIP, vmName, controlPlaneIP, joinToken, discoveryHash string) error {
	fmt.Printf("Joining %s to cluster...\n", vmName)

	script := fmt.Sprintf(`
set -e
sudo kubeadm join %s:6443 \
  --token %s \
  --discovery-token-ca-cert-hash %s
`, controlPlaneIP, joinToken, discoveryHash)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_, stderr, err := m.sshClient.ExecuteWithContext(ctx, vmIP, script)
	if err != nil {
		return fmt.Errorf("node join failed: %w, stderr: %s", err, stderr)
	}

	fmt.Printf("✓ %s joined cluster\n", vmName)
	return nil
}

// GetJoinCommand retrieves the join command from the control plane
func (m *K8sMachineManager) GetJoinCommand(controlPlaneIP string) (token, hash string, err error) {
	script := `
set -e
# Generate new token
TOKEN=$(sudo kubeadm token create)

# Get CA cert hash
HASH=$(openssl x509 -pubkey -in /etc/kubernetes/pki/ca.crt | \
       openssl rsa -pubin -outform der 2>/dev/null | \
       openssl dgst -sha256 -hex | sed 's/^.* //')

echo "TOKEN:$TOKEN"
echo "HASH:sha256:$HASH"
`

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	stdout, stderr, err := m.sshClient.ExecuteWithContext(ctx, controlPlaneIP, script)
	if err != nil {
		return "", "", fmt.Errorf("failed to get join command: %w, stderr: %s", err, stderr)
	}

	lines := strings.Split(stdout, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "TOKEN:") {
			token = strings.TrimPrefix(line, "TOKEN:")
		} else if strings.HasPrefix(line, "HASH:") {
			hash = strings.TrimPrefix(line, "HASH:")
		}
	}

	if token == "" || hash == "" {
		return "", "", fmt.Errorf("failed to parse join command from output")
	}

	return strings.TrimSpace(token), strings.TrimSpace(hash), nil
}

// extractJoinCommand extracts the kubeadm join command from kubeadm init output
func extractJoinCommand(output string) string {
	lines := strings.Split(output, "\n")
	var joinCmd strings.Builder
	recording := false

	for _, line := range lines {
		if strings.Contains(line, "kubeadm join") {
			recording = true
		}
		if recording {
			joinCmd.WriteString(line)
			joinCmd.WriteString("\n")
			if strings.Contains(line, "discovery-token-ca-cert-hash") {
				break
			}
		}
	}

	return joinCmd.String()
}

// GetKubeconfig retrieves the kubeconfig from a control plane node
func (m *K8sMachineManager) GetKubeconfig(controlPlaneIP, outputPath string) error {
	script := "sudo cat /etc/kubernetes/admin.conf"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stdout, stderr, err := m.sshClient.ExecuteWithContext(ctx, controlPlaneIP, script)
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig: %w, stderr: %s", err, stderr)
	}

	// Write to file
	if err := os.WriteFile(outputPath, []byte(stdout), 0600); err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	fmt.Printf("✓ Kubeconfig saved to: %s\n", outputPath)
	return nil
}
