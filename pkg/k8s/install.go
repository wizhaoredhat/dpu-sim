// Package k8s provides functions to install Kubernetes on a machine
package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wizhao/dpu-sim/pkg/network"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

// GetLinuxDistro detects the Linux distribution of a remote machine via SSH
func (m *K8sMachineManager) GetLinuxDistro(machineIP string) (*platform.Distro, error) {
	return platform.Detect(m.sshClient, machineIP)
}

// InstallKubernetes installs Kubernetes on a machine (baremetal or VM)
func (m *K8sMachineManager) InstallKubernetes(machineIP, machineName, k8sVersion string) error {
	fmt.Printf("Installing Kubernetes on %s (%s)...\n", machineName, machineIP)

	if err := m.sshClient.WaitForSSH(machineIP, 5*time.Minute); err != nil {
		return fmt.Errorf("failed to wait for SSH: %w", err)
	}

	// Get Linux distribution
	linuxDistro, err := m.GetLinuxDistro(machineIP)
	if err != nil {
		return fmt.Errorf("failed to get Linux distribution: %w", err)
	}
	m.linuxDistro = linuxDistro
	fmt.Printf("✓ Detected Linux distribution: %s %s (package manager: %s, architecture: %s) on %s\n", m.linuxDistro.ID, m.linuxDistro.VersionID, m.linuxDistro.PackageManager, m.linuxDistro.Architecture, machineName)

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

	if err := m.installKubelet(machineIP); err != nil {
		return fmt.Errorf("failed to install Kubernetes Tools: %w", err)
	}

	if err := m.disableFirewall(machineIP); err != nil {
		return fmt.Errorf("failed to disable firewall: %w", err)
	}

	if err := m.verifyK8sOvSInstallation(machineIP); err != nil {
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

	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	sb.WriteString("sudo swapoff -a\n")
	sb.WriteString("sudo sed -i '/ swap / s/^/#/' /etc/fstab\n")

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, sb.String(), 5*time.Minute)
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

	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	sb.WriteString("sudo tee /etc/modules-load.d/k8s.conf > /dev/null <<EOF\n")
	sb.WriteString("overlay\n")
	sb.WriteString("br_netfilter\n")
	sb.WriteString("EOF\n")

	sb.WriteString("sudo modprobe overlay\n")
	sb.WriteString("sudo modprobe br_netfilter\n")

	// Enable IPv4 packets to be routed between interfaces
	sb.WriteString("sudo tee /etc/sysctl.d/k8s.conf > /dev/null <<EOF\n")
	sb.WriteString("net.bridge.bridge-nf-call-iptables = 1\n")
	sb.WriteString("net.bridge.bridge-nf-call-ip6tables = 1\n")
	sb.WriteString("net.ipv4.ip_forward = 1\n")
	sb.WriteString("EOF\n")

	// Apply sysctl params without reboot
	sb.WriteString("sudo sysctl --system > /dev/null 2>&1\n")

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, sb.String(), 5*time.Minute)
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

	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	if m.linuxDistro.PackageManager == platform.DNF {
		sb.WriteString("sudo tee /etc/yum.repos.d/cri-o.repo > /dev/null <<EOF\n")
		sb.WriteString("[cri-o]\n")
		sb.WriteString("name=CRI-O\n")
		sb.WriteString(fmt.Sprintf("baseurl=https://pkgs.k8s.io/addons:/cri-o:/stable:/v%s/rpm/\n", k8sVersion))
		sb.WriteString("enabled=1\n")
		sb.WriteString("gpgcheck=1\n")
		sb.WriteString(fmt.Sprintf("gpgkey=https://pkgs.k8s.io/addons:/cri-o:/stable:/v%s/rpm/repodata/repomd.xml.key\n", k8sVersion))
		sb.WriteString("EOF\n")

		// Install CRI-O, iproute-tc, and containernetworking-plugins (standard CNI plugins like bridge, host-local, etc.)
		sb.WriteString(fmt.Sprintf("sudo %s install -y cri-o iproute-tc containernetworking-plugins > /dev/null 2>&1 && \n", platform.DNF))

		// On Fedora, CNI plugins are installed to /usr/libexec/cni/ but CRI-O looks in /opt/cni/bin/
		// Create symlinks so CRI-O can find them
		sb.WriteString("sudo mkdir -p /opt/cni/bin && \n")
		sb.WriteString("sudo ln -sf /usr/libexec/cni/* /opt/cni/bin/ && \n")
	} else {
		return fmt.Errorf("unsupported Linux distribution: %s", m.linuxDistro.ID)
	}
	sb.WriteString("sudo systemctl enable crio > /dev/null 2>&1 && \n")
	sb.WriteString("sudo systemctl start crio\n")

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, sb.String(), 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to install CRI-O: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	fmt.Printf("✓ CRI-O installed\n")
	return nil
}

func (m *K8sMachineManager) installOpenVSwitch(machineIP string) error {
	fmt.Printf("Installing Open vSwitch on %s...\n", machineIP)

	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	if m.linuxDistro.PackageManager == platform.DNF {
		sb.WriteString(fmt.Sprintf("sudo %s install -y NetworkManager-ovs > /dev/null 2>&1 && \n", platform.DNF))
		sb.WriteString(fmt.Sprintf("sudo %s install -y openvswitch > /dev/null 2>&1 && \n", platform.DNF))
	} else {
		return fmt.Errorf("unsupported Linux distribution: %s", m.linuxDistro.ID)
	}
	sb.WriteString("sudo systemctl enable openvswitch > /dev/null 2>&1 && \n")
	sb.WriteString("sudo systemctl restart NetworkManager > /dev/null 2>&1 && \n")
	sb.WriteString("sudo systemctl start openvswitch\n")

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, sb.String(), 5*time.Minute)
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

	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	if m.linuxDistro.PackageManager == platform.DNF {
		sb.WriteString("sudo tee /etc/yum.repos.d/kubernetes.repo > /dev/null <<EOF\n")
		sb.WriteString("[kubernetes]\n")
		sb.WriteString("name=Kubernetes\n")
		sb.WriteString(fmt.Sprintf("baseurl=https://pkgs.k8s.io/core:/stable:/v%s/rpm/\n", k8sVersion))
		sb.WriteString("enabled=1\n")
		sb.WriteString("gpgcheck=1\n")
		sb.WriteString(fmt.Sprintf("gpgkey=https://pkgs.k8s.io/core:/stable:/v%s/rpm/repodata/repomd.xml.key\n", k8sVersion))
		sb.WriteString("exclude=kubelet kubeadm kubectl cri-tools kubernetes-cni\n")
		sb.WriteString("EOF\n")
	} else {
		return fmt.Errorf("unsupported Linux distribution: %s", m.linuxDistro.ID)
	}

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, sb.String(), 5*time.Minute)
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
func (m *K8sMachineManager) installKubelet(machineIP string) error {
	fmt.Printf("Installing Kubelet, Kubeadm, Kubectl on %s...\n", machineIP)

	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	if m.linuxDistro.PackageManager == platform.DNF {
		sb.WriteString(fmt.Sprintf("sudo %s install -y kubelet kubeadm kubectl --setopt=disable_excludes=kubernetes > /dev/null 2>&1 && \n", platform.DNF))
	} else {
		return fmt.Errorf("unsupported Linux distribution: %s", m.linuxDistro.ID)
	}
	sb.WriteString("sudo systemctl enable kubelet > /dev/null 2>&1\n")

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, sb.String(), 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to install Kubelet: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	fmt.Printf("✓ Kubelet installed\n")
	return nil
}

// Disable firewall on the machine
func (m *K8sMachineManager) disableFirewall(machineIP string) error {
	fmt.Printf("Disabling firewall on %s...\n", machineIP)

	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	if m.linuxDistro.PackageManager == platform.DNF {
		// Check if firewalld is installed before trying to disable/remove it
		sb.WriteString("if rpm -q firewalld &>/dev/null; then\n")
		sb.WriteString("  sudo systemctl disable --now firewalld\n")
		sb.WriteString(fmt.Sprintf("  sudo %s remove -y firewalld\n", platform.DNF))
		sb.WriteString("fi\n")
	} else {
		return fmt.Errorf("unsupported Linux distribution: %s", m.linuxDistro.ID)
	}

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, sb.String(), 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to configure firewall: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	fmt.Printf("✓ Firewall disabled\n")
	return nil
}

// Verify Kubernetes installation on the machine
func (m *K8sMachineManager) verifyK8sOvSInstallation(machineIP string) error {
	fmt.Printf("Verifying Kubernetes installation on %s...\n", machineIP)

	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	sb.WriteString("sudo kubeadm version -o short 2>/dev/null\n")

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, sb.String(), 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to verify Kubernetes installation: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}
	fmt.Printf("✓ Kubeadm version: %s\n", strings.TrimSpace(stdout))

	sb.Reset()
	sb.WriteString("sudo systemctl is-active crio\n")

	stdout, stderr, err = m.sshClient.ExecuteWithTimeout(machineIP, sb.String(), 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to verify CRI-O installation: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}
	fmt.Printf("✓ CRI-O is active\n")

	sb.Reset()
	sb.WriteString("set -e\n")
	sb.WriteString("sudo ovs-vsctl --version | head -n 1\n")

	stdout, stderr, err = m.sshClient.ExecuteWithTimeout(machineIP, sb.String(), 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to verify Open vSwitch installation: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	fmt.Printf("✓ Open vSwitch version: %s\n", strings.TrimSpace(stdout))
	return nil
}

func (m *K8sMachineManager) SetupOVNBrEx(machineIP string, mgmtIP string, k8sIP string) error {
	fmt.Printf("Setting up OVN br-ex on %s...\n", machineIP)

	mgmtInterfaceInfo, err := network.GetInterfaceByIP(m.sshClient, machineIP, mgmtIP)
	if err != nil {
		return fmt.Errorf("failed to get interface information: %w", err)
	}
	fmt.Printf("Mgmt Interface information: %s\n", mgmtInterfaceInfo.String())

	k8sInterfaceInfo, err := network.GetInterfaceByIP(m.sshClient, machineIP, k8sIP)
	if err != nil {
		return fmt.Errorf("failed to get interface information: %w", err)
	}
	fmt.Printf("K8s Interface information: %s\n", k8sInterfaceInfo.String())

	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	sb.WriteString("BRIDGE_NAME=br-ex\n")
	sb.WriteString(fmt.Sprintf("IF1=%s\n", mgmtInterfaceInfo.Name))
	sb.WriteString("IF1_CONN=$(nmcli -g GENERAL.CONNECTION device show $IF1 2>/dev/null || echo '')\n")
	// Check if IF1_CONN is a valid NetworkManager connection
	sb.WriteString("IF1_CONN_EXISTS=$(nmcli -g NAME connection show \"$IF1_CONN\" 2>/dev/null || echo '')\n")
	sb.WriteString(fmt.Sprintf("IF2=%s\n", k8sInterfaceInfo.Name))
	sb.WriteString("IF2_MAC=$(cat /sys/class/net/$IF2/address)\n")
	// Get the actual connection name for IF2 (may differ from interface name)
	sb.WriteString("IF2_CONN=$(nmcli -g GENERAL.CONNECTION device show $IF2 2>/dev/null || echo '')\n")

	sb.WriteString("nmcli c add type ovs-bridge conn.interface $BRIDGE_NAME con-name $BRIDGE_NAME\n")
	sb.WriteString("nmcli c add type ovs-port conn.interface $BRIDGE_NAME master $BRIDGE_NAME con-name ovs-port-$BRIDGE_NAME\n")
	sb.WriteString("nmcli c add type ovs-interface slave-type ovs-port conn.interface $BRIDGE_NAME master ovs-port-$BRIDGE_NAME con-name ovs-if-$BRIDGE_NAME\n")
	sb.WriteString("nmcli c add type ovs-port conn.interface $IF2 master $BRIDGE_NAME con-name ovs-port-$IF2\n")
	sb.WriteString("nmcli c add type ethernet conn.interface $IF2 master ovs-port-$IF2 con-name ovs-if-$IF2\n")
	// Only delete the old connection if it exists and is not empty
	sb.WriteString("if [ -n \"$IF2_CONN\" ] && [ \"$IF2_CONN\" != \"--\" ]; then nmcli conn delete \"$IF2_CONN\"; fi\n")
	sb.WriteString("sudo ip addr flush dev $IF2\n")
	sb.WriteString("nmcli conn mod $BRIDGE_NAME connection.autoconnect yes\n")
	sb.WriteString("nmcli conn mod ovs-if-$IF2 connection.autoconnect yes\n")
	sb.WriteString("nmcli conn mod ovs-port-$IF2 connection.autoconnect yes\n")
	sb.WriteString("nmcli conn mod ovs-if-$BRIDGE_NAME connection.autoconnect yes\n")
	sb.WriteString("nmcli conn mod ovs-port-$BRIDGE_NAME connection.autoconnect yes\n")

	// Set the br-ex interface to use DHCP
	sb.WriteString("nmcli conn mod ovs-if-$BRIDGE_NAME ipv4.method auto\n")
	sb.WriteString("nmcli conn mod ovs-if-$BRIDGE_NAME ipv4.route-metric 50\n")

	// Set the br-ex interface to be the default route
	sb.WriteString("nmcli conn mod ovs-if-$BRIDGE_NAME ipv4.never-default no\n")

	// Set the MAC address of the br-ex interface to the MAC address of the IF2 interface
	// to get the same DHCP lease on the br-ex interface as the IF2 interface
	sb.WriteString("nmcli conn mod ovs-if-$BRIDGE_NAME 802-3-ethernet.cloned-mac-address $IF2_MAC\n")

	// Make sure the MGMT interface is not the default route (only if IF1_CONN is a valid NM connection)
	sb.WriteString("if [ -n \"$IF1_CONN_EXISTS\" ]; then\n")
	sb.WriteString("  nmcli conn mod \"$IF1_CONN\" ipv4.never-default yes\n")
	sb.WriteString("  nmcli conn mod \"$IF1_CONN\" ipv4.ignore-auto-dns yes\n")
	sb.WriteString("  nmcli conn up \"$IF1_CONN\"\n")
	sb.WriteString("fi\n")

	// Activate the OVS connections immediately (order matters: bridge first, then ports)
	sb.WriteString("nmcli conn up $BRIDGE_NAME\n")
	sb.WriteString("nmcli conn up ovs-if-$IF2\n")
	sb.WriteString("nmcli conn up ovs-port-$IF2\n")
	sb.WriteString("nmcli conn up ovs-if-$BRIDGE_NAME\n")
	sb.WriteString("nmcli conn up ovs-port-$BRIDGE_NAME\n")

	// Known issue for br-int bridge (not properly created by OVN)
	sb.WriteString("ovs-vsctl add-br br-int\n")

	// Set br-ex as the external bridge for OVN
	sb.WriteString("sudo ovs-vsctl set open_vswitch . external-ids:ovn-bridge-mappings=\"physnet1:br-ex\"\n")

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, sb.String(), 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to setup OVN br-ex: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}
	return nil
}

// PrintOVNBrExStatus prints the status of the OVN br-ex bridge
// Does not return an error, just prints the status.
func (m *K8sMachineManager) PrintOVNBrExStatus(machineIP string) {
	fmt.Printf("\n========== OVN/OVS Status on %s ==========\n\n", machineIP)

	fmt.Println("--- OVS Bridges ---")
	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(machineIP, "sudo ovs-vsctl list-br", 30*time.Second)
	if err != nil {
		fmt.Printf("Error listing bridges: %s\n", stderr)
	} else {
		fmt.Println(strings.TrimSpace(stdout))
	}

	fmt.Println("--- br-ex Ports ---")
	stdout, stderr, err = m.sshClient.ExecuteWithTimeout(machineIP, "sudo ovs-vsctl list-ports br-ex 2>/dev/null || echo 'br-ex not found'", 30*time.Second)
	if err != nil {
		fmt.Printf("Error: %s\n", stderr)
	} else {
		fmt.Println(strings.TrimSpace(stdout))
	}

	fmt.Println("--- OVS Show (Full Config) ---")
	stdout, stderr, err = m.sshClient.ExecuteWithTimeout(machineIP, "sudo ovs-vsctl show", 30*time.Second)
	if err != nil {
		fmt.Printf("Error: %s\n", stderr)
	} else {
		fmt.Println(strings.TrimSpace(stdout))
	}

	stdout, stderr, err = m.sshClient.ExecuteWithTimeout(machineIP, "ip route show", 30*time.Second)
	if err != nil {
		fmt.Printf("Error: %s\n", stderr)
	} else {
		fmt.Println(strings.TrimSpace(stdout))
	}

	fmt.Println("--- br-ex Linux Interface ---")
	stdout, stderr, err = m.sshClient.ExecuteWithTimeout(machineIP, "ip addr show br-ex 2>/dev/null || echo 'br-ex interface not found'", 30*time.Second)
	if err != nil {
		fmt.Printf("Error: %s\n", stderr)
	} else {
		fmt.Println(strings.TrimSpace(stdout))
	}

	fmt.Println("--- NetworkManager Connections ---")
	stdout, stderr, err = m.sshClient.ExecuteWithTimeout(machineIP, "nmcli connection show 2>/dev/null || echo 'nmcli not available'", 30*time.Second)
	if err != nil {
		fmt.Printf("Error: %s\n", stderr)
	} else {
		fmt.Println(strings.TrimSpace(stdout))
	}

	fmt.Println("--- OVS External IDs (Open_vSwitch) ---")
	stdout, stderr, err = m.sshClient.ExecuteWithTimeout(machineIP, "sudo ovs-vsctl get Open_vSwitch . external_ids", 30*time.Second)
	if err != nil {
		fmt.Printf("Error: %s\n", stderr)
	} else {
		fmt.Println(strings.TrimSpace(stdout))
	}

	fmt.Println("--- br-ex External IDs ---")
	stdout, stderr, err = m.sshClient.ExecuteWithTimeout(machineIP, "sudo ovs-vsctl get Bridge br-ex external_ids 2>/dev/null || echo 'br-ex not found'", 30*time.Second)
	if err != nil {
		fmt.Printf("Error: %s\n", stderr)
	} else {
		fmt.Println(strings.TrimSpace(stdout))
	}

	fmt.Println("--- br-int External IDs ---")
	stdout, stderr, err = m.sshClient.ExecuteWithTimeout(machineIP, "sudo ovs-vsctl get Bridge br-int external_ids 2>/dev/null || echo 'br-int not found'", 30*time.Second)
	if err != nil {
		fmt.Printf("Error: %s\n", stderr)
	} else {
		fmt.Println(strings.TrimSpace(stdout))
	}

	fmt.Println("==========================================")
}

func (m *K8sMachineManager) SetupKubectlForRootUser(machineName, mgmtIP string) error {
	fmt.Printf("Setting up kubectl on %s...\n", machineName)

	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	sb.WriteString("mkdir -p /root/.kube\n")
	sb.WriteString("sudo cp /etc/kubernetes/admin.conf /root/.kube/config\n")
	sb.WriteString("sudo chown root:root /root/.kube/config\n")

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(mgmtIP, sb.String(), 1*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to setup kubectl: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}
	return nil
}

// ExtractWorkerJoinCommand extracts the worker join command from the machine
func (m *K8sMachineManager) ExtractWorkerJoinCommand(machineName, mgmtIP string) (string, error) {
	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	sb.WriteString("sudo kubeadm token create --print-join-command\n")

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(mgmtIP, sb.String(), 1*time.Minute)
	if err != nil {
		return "", fmt.Errorf("failed to extract join command: %w, stderr: %s", err, stderr)
	}
	workerJoinCommand := strings.TrimSpace(stdout)
	return workerJoinCommand, nil
}

// GenerateCertificateKey generates a new certificate key for control plane joins
func (m *K8sMachineManager) GenerateCertificateKey(machineName, mgmtIP string) (string, error) {
	// Generate a new certificate key for control plane joins
	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	sb.WriteString("sudo kubeadm init phase upload-certs --upload-certs 2>/dev/null | tail -1\n")

	stdout, stderr, err := m.sshClient.ExecuteWithTimeout(mgmtIP, sb.String(), 1*time.Minute)
	if err != nil {
		return "", fmt.Errorf("failed to get certificate key: %w, stderr: %s", err, stderr)
	}
	certificateKey := strings.TrimSpace(stdout)

	return certificateKey, nil
}

// InitializeControlPlane initializes a Kubernetes control plane node
// Returns ControlPlaneInfo with all information needed to join additional nodes
func (m *K8sMachineManager) InitializeControlPlane(machineName, mgmtIP, k8sIP, podCIDR, serviceCIDR string) (*ControlPlaneInfo, error) {
	fmt.Printf("Initializing control plane on %s...\n", machineName)
	fmt.Printf("Mgmt IP: %s K8s IP: %s Pod CIDR: %s, Service CIDR: %s\n", mgmtIP, k8sIP, podCIDR, serviceCIDR)

	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	// Use --upload-certs to enable control plane join for additional masters
	sb.WriteString(fmt.Sprintf("sudo kubeadm init --pod-network-cidr=%s --service-cidr=%s --apiserver-advertise-address=%s --upload-certs\n", podCIDR, serviceCIDR, k8sIP))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	stdout, stderr, err := m.sshClient.ExecuteWithContext(ctx, mgmtIP, sb.String())
	if err != nil {
		return nil, fmt.Errorf("control plane initialization failed: %w, stderr: %s", err, stderr)
	}

	fmt.Printf("Control plane initialization output: %s\n", stdout)

	if err := m.SetupKubectlForRootUser(machineName, mgmtIP); err != nil {
		return nil, fmt.Errorf("failed to setup kubectl for root user: %w", err)
	}

	workerJoinCommand, err := m.ExtractWorkerJoinCommand(machineName, mgmtIP)
	if err != nil {
		return nil, fmt.Errorf("failed to extract worker join command: %w", err)
	}

	certificateKey, err := m.GenerateCertificateKey(machineName, mgmtIP)
	if err != nil {
		return nil, fmt.Errorf("failed to generate certificate key: %w", err)
	}

	// Build the control plane join command
	controlPlaneJoinCommand := fmt.Sprintf("%s --control-plane --certificate-key %s", workerJoinCommand, certificateKey)

	// Get the kubeconfig for API access
	kubeconfig, err := m.getKubeconfigContent(mgmtIP)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	// Build the API server endpoint
	apiServerEndpoint := fmt.Sprintf("https://%s:6443", k8sIP)

	joinInfo := &ControlPlaneInfo{
		WorkerJoinCommand:       workerJoinCommand,
		ControlPlaneJoinCommand: controlPlaneJoinCommand,
		APIServerEndpoint:       apiServerEndpoint,
		Kubeconfig:              kubeconfig,
	}

	fmt.Printf("✓ Control plane initialized on %s\n", machineName)
	fmt.Printf("Worker join command: %s\n", workerJoinCommand)
	fmt.Printf("Control plane join command: %s\n", controlPlaneJoinCommand)
	fmt.Printf("API server endpoint: %s\n", apiServerEndpoint)
	return joinInfo, nil
}

// JoinControlPlane joins an additional control plane node to a Kubernetes cluster
func (m *K8sMachineManager) JoinControlPlane(machineName, mgmtIP string, joinInfo *ControlPlaneInfo) error {
	fmt.Printf("Joining control plane node %s to Kubernetes cluster...\n", machineName)

	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	sb.WriteString(fmt.Sprintf("sudo %s\n", joinInfo.ControlPlaneJoinCommand))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	stdout, stderr, err := m.sshClient.ExecuteWithContext(ctx, mgmtIP, sb.String())
	if err != nil {
		return fmt.Errorf("failed to join control plane node: %w, stderr: %s", err, stderr)
	}

	if err := m.SetupKubectlForRootUser(machineName, mgmtIP); err != nil {
		return fmt.Errorf("failed to setup kubectl for root user: %w", err)
	}

	fmt.Printf("✓ Control plane node joined to Kubernetes cluster: %s\n", machineName)
	fmt.Printf("Join command output: %s\n", stdout)
	return nil
}

// JoinWorker joins a worker node to a Kubernetes cluster
func (m *K8sMachineManager) JoinWorker(machineName, mgmtIP string, joinInfo *ControlPlaneInfo) error {
	fmt.Printf("Joining worker node %s to Kubernetes cluster...\n", machineName)

	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	sb.WriteString(fmt.Sprintf("sudo %s\n", joinInfo.WorkerJoinCommand))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	stdout, stderr, err := m.sshClient.ExecuteWithContext(ctx, mgmtIP, sb.String())
	if err != nil {
		return fmt.Errorf("failed to join worker node: %w, stderr: %s", err, stderr)
	}

	fmt.Printf("✓ Worker node joined to Kubernetes cluster: %s\n", machineName)
	fmt.Printf("Join command output: %s\n", stdout)
	return nil
}

// getKubeconfigContent retrieves the kubeconfig content from a control plane node
func (m *K8sMachineManager) getKubeconfigContent(controlPlaneIP string) (string, error) {
	script := "sudo cat /etc/kubernetes/admin.conf"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stdout, stderr, err := m.sshClient.ExecuteWithContext(ctx, controlPlaneIP, script)
	if err != nil {
		return "", fmt.Errorf("failed to get kubeconfig: %w, stderr: %s", err, stderr)
	}

	return strings.TrimSpace(stdout), nil
}

// GetKubeconfig retrieves the kubeconfig from a control plane node and saves it to a file
func (m *K8sMachineManager) GetKubeconfig(controlPlaneIP, outputPath string) error {
	kubeconfig, err := m.getKubeconfigContent(controlPlaneIP)
	if err != nil {
		return err
	}

	// Write to file
	if err := os.WriteFile(outputPath, []byte(kubeconfig), 0600); err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	fmt.Printf("✓ Kubeconfig saved to: %s\n", outputPath)
	return nil
}

// SaveKubeconfigToFile saves kubeconfig content to a file in the specified directory
// The file is saved as <kubeconfigDir>/<clusterName>.kubeconfig
func SaveKubeconfigToFile(kubeconfigContent, clusterName, kubeconfigDir string) error {
	// Create kubeconfig directory if it doesn't exist
	if err := os.MkdirAll(kubeconfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create kubeconfig directory: %w", err)
	}

	// Build the file path
	filepath := getKubeconfigPath(clusterName, kubeconfigDir)

	// Write kubeconfig to file with restricted permissions
	if err := os.WriteFile(filepath, []byte(kubeconfigContent), 0600); err != nil {
		return fmt.Errorf("failed to write kubeconfig file: %w", err)
	}

	fmt.Printf("✓ Kubeconfig saved to: %s\n", filepath)
	return nil
}

// getKubeconfigPath returns the path to the kubeconfig file for a given cluster name
// The path is <kubeconfigDir>/<clusterName>.kubeconfig
func getKubeconfigPath(clusterName, kubeconfigDir string) string {
	filename := fmt.Sprintf("%s.kubeconfig", clusterName)
	return fmt.Sprintf("%s/%s", kubeconfigDir, filename)
}

// FindKubeconfig returns the kubeconfig file path for a cluster if it exists
// Returns the path and nil error if found, or empty string and error if not found
func findKubeconfig(clusterName, kubeconfigDir string) (string, error) {
	filepath := getKubeconfigPath(clusterName, kubeconfigDir)

	if _, err := os.Stat(filepath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("kubeconfig for cluster %s not found at %s", clusterName, filepath)
		}
		return "", fmt.Errorf("failed to check kubeconfig file: %w", err)
	}

	return filepath, nil
}

// ReadKubeconfigFile reads and returns the kubeconfig content for a given cluster
func ReadKubeconfigFile(clusterName, kubeconfigDir string) (string, error) {
	filepath, err := findKubeconfig(clusterName, kubeconfigDir)
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(filepath)
	if err != nil {
		return "", fmt.Errorf("failed to read kubeconfig file: %w", err)
	}

	return string(content), nil
}

func CleanupKubeconfig(kubeconfigDir string) error {
	// Find all kubeconfig files in the directory
	files, err := filepath.Glob(filepath.Join(kubeconfigDir, "*.kubeconfig"))
	if err != nil {
		return fmt.Errorf("failed to glob kubeconfig files: %w", err)
	}

	// Remove each file individually
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			return fmt.Errorf("failed to remove kubeconfig file %s: %w", file, err)
		}
		fmt.Printf("✓ Kubeconfig file removed: %s\n", file)
	}

	return nil
}
