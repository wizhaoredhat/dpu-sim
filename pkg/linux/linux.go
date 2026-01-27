package linux

import (
	"fmt"
	"strings"
	"time"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

// Sets the hostname on the target machine
func SetHostname(cmdExec platform.CommandExecutor, hostname string) error {
	fmt.Printf("Setting hostname to %s on %s...\n", hostname, cmdExec.String())

	script := fmt.Sprintf("sudo hostnamectl set-hostname %s", hostname)

	stdout, stderr, err := cmdExec.ExecuteWithTimeout(script, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to set hostname: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	fmt.Printf("✓ Hostname set to %s\n", hostname)
	return nil
}

// InstallGenericPackage installs a package dependency using the package manager
func InstallGenericPackage(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	switch distro.PackageManager {
	case platform.DNF:
		if err := cmdExec.RunCmd("sudo", platform.DNF, "install", "-y", dep.Name); err != nil {
			return fmt.Errorf("failed to install %s: %w", dep.Name, err)
		}
	case platform.APT:
		if err := cmdExec.RunCmd("sudo", platform.APT, "install", "-y", dep.Name); err != nil {
			return fmt.Errorf("failed to install %s: %w", dep.Name, err)
		}
	default:
		return platform.UnsupportedPackageManager(distro)
	}
	return nil
}

// CheckGenericPackage checks if a package dependency is installed using the package manager
func CheckGenericPackage(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	switch distro.PackageManager {
	case platform.DNF:
		if err := cmdExec.RunCmd("rpm", "-q", dep.Name); err != nil {
			return fmt.Errorf("package %s is not installed: %w", dep.Name, err)
		}
	case platform.APT:
		if err := cmdExec.RunCmd("dpkg", "-s", dep.Name); err != nil {
			return fmt.Errorf("package %s is not installed: %w", dep.Name, err)
		}
	default:
		return platform.UnsupportedPackageManager(distro)
	}
	return nil
}

// Disables swap on the target machine
func DisableSwap(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	sb.WriteString("sudo swapoff -a\n")
	sb.WriteString("sudo sed -i '/ swap / s/^/#/' /etc/fstab\n")

	stdout, stderr, err := cmdExec.Execute(sb.String())
	if err != nil {
		return fmt.Errorf("failed to disable swap: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}
	return nil
}

// Check if swap is disabled on the target machine
func CheckSwapDisabled(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	stdout, stderr, err := cmdExec.Execute("swapon --show")
	if err != nil {
		return fmt.Errorf("swapon failed: %w, stderr: %s", err, stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		return fmt.Errorf("swap is not disabled: %s", stdout)
	}
	return nil
}

// Configure kernel modules on the target machine for Kubernetes
func ConfigureK8sKernelModules(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	// Load kernel modules
	loadModulesContent := strings.Builder{}
	loadModulesContent.WriteString("overlay\n")
	loadModulesContent.WriteString("br_netfilter\n")
	loadModulesContent.WriteString("EOF\n")
	if err := cmdExec.WriteFile("/etc/modules-load.d/k8s.conf", []byte(loadModulesContent.String()), 0644); err != nil {
		return fmt.Errorf("failed to write modules load file: %w", err)
	}
	// Load kernel modules
	if err := cmdExec.RunCmd("sudo", "modprobe", "overlay"); err != nil {
		return fmt.Errorf("failed to modprobe overlay: %w", err)
	}
	if err := cmdExec.RunCmd("sudo", "modprobe", "br_netfilter"); err != nil {
		return fmt.Errorf("failed to modprobe br_netfilter: %w", err)
	}
	// Enable IPv4 packets to be routed between interfaces
	sysctlContent := strings.Builder{}
	sysctlContent.WriteString("net.bridge.bridge-nf-call-iptables = 1\n")
	sysctlContent.WriteString("net.bridge.bridge-nf-call-ip6tables = 1\n")
	sysctlContent.WriteString("net.ipv4.ip_forward = 1\n")
	if err := cmdExec.WriteFile("/etc/sysctl.d/k8s.conf", []byte(sysctlContent.String()), 0644); err != nil {
		return fmt.Errorf("failed to write sysctl file: %w", err)
	}
	// Apply sysctl params without reboot
	if err := cmdExec.RunCmd("sudo", "sysctl", "--system"); err != nil {
		return fmt.Errorf("failed to apply sysctl params: %w", err)
	}
	return nil
}

// Check if Kubernetes specific kernel modules are loaded on the target machine
func CheckK8sKernelModules(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	stdout, stderr, err := cmdExec.Execute("lsmod | grep overlay")
	if err != nil {
		return fmt.Errorf("failed to check overlay module: %w, stderr: %s", err, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		return fmt.Errorf("overlay kernel module is not loaded")
	}

	stdout, stderr, err = cmdExec.Execute("lsmod | grep br_netfilter")
	if err != nil {
		return fmt.Errorf("failed to check br_netfilter module: %w, stderr: %s", err, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		return fmt.Errorf("br_netfilter kernel module is not loaded")
	}
	return nil
}

// Install CRI-O on the target machine
func InstallCRIO(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	k8sVersion := cfg.Kubernetes.Version
	if distro.PackageManager == platform.DNF {
		// Add CRI-O repository
		repoContent := strings.Builder{}
		repoContent.WriteString("[cri-o]\n")
		repoContent.WriteString("name=CRI-O\n")
		repoContent.WriteString(fmt.Sprintf("baseurl=https://pkgs.k8s.io/addons:/cri-o:/stable:/v%s/rpm/\n", k8sVersion))
		repoContent.WriteString("enabled=1\n")
		repoContent.WriteString("gpgcheck=1\n")
		repoContent.WriteString(fmt.Sprintf("gpgkey=https://pkgs.k8s.io/addons:/cri-o:/stable:/v%s/rpm/repodata/repomd.xml.key\n", k8sVersion))
		if err := cmdExec.WriteFile("/etc/yum.repos.d/cri-o.repo", []byte(repoContent.String()), 0644); err != nil {
			return fmt.Errorf("failed to write cri-o repo file: %w", err)
		}
		// Install CRI-O, iproute-tc, and containernetworking-plugins (standard CNI plugins like bridge, host-local, etc.)
		if err := cmdExec.RunCmd("sudo", platform.DNF, "install", "-y", "cri-o", "iproute-tc", "containernetworking-plugins"); err != nil {
			return fmt.Errorf("failed to install CRI-O: %w", err)
		}
		// On Fedora, CNI plugins are installed to /usr/libexec/cni/ but CRI-O looks in /opt/cni/bin/
		// Create symlinks so CRI-O can find them
		if err := cmdExec.RunCmd("sudo", "mkdir", "-p", "/opt/cni/bin"); err != nil {
			return fmt.Errorf("failed to create /opt/cni/bin: %w", err)
		}
		if err := cmdExec.RunCmd("sudo", "ln", "-sf", "/usr/libexec/cni/*", "/opt/cni/bin/"); err != nil {
			return fmt.Errorf("failed to create symlinks: %w", err)
		}
	} else {
		return platform.UnsupportedPackageManager(distro)
	}
	if err := cmdExec.RunCmd("sudo", "systemctl", "enable", "crio"); err != nil {
		return fmt.Errorf("failed to enable CRI-O: %w", err)
	}
	if err := cmdExec.RunCmd("sudo", "systemctl", "start", "crio"); err != nil {
		return fmt.Errorf("failed to start CRI-O: %w", err)
	}
	return nil
}

// Install Open vSwitch on the target machine
func InstallOpenVSwitch(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	switch distro.PackageManager {
	case platform.DNF:
		if distro.Architecture == platform.X86_64 {
			_ = cmdExec.RunCmd("sudo", "subscription-manager", "repos", "--enable=openstack-17-for-rhel-9-x86_64-rpms")
		} else {
			return platform.UnsupportedArchitecture(distro)
		}
		if err := cmdExec.RunCmd("sudo", platform.DNF, "install", "-y", "openvswitch"); err != nil {
			return fmt.Errorf("failed to install openvswitch: %w", err)
		}
	case platform.APT:
		if err := cmdExec.RunCmd("sudo", platform.APT, "install", "-y", "openvswitch-switch"); err != nil {
			return fmt.Errorf("failed to install openvswitch: %w", err)
		}
	default:
		return platform.UnsupportedPackageManager(distro)
	}
	if err := cmdExec.RunCmd("sudo", "systemctl", "enable", "openvswitch"); err != nil {
		return fmt.Errorf("failed to enable openvswitch: %w", err)
	}
	if err := cmdExec.RunCmd("sudo", "systemctl", "restart", "NetworkManager"); err != nil {
		return fmt.Errorf("failed to restart NetworkManager: %w", err)
	}
	if err := cmdExec.RunCmd("sudo", "systemctl", "start", "openvswitch"); err != nil {
		return fmt.Errorf("failed to start openvswitch: %w", err)
	}
	return nil
}

// Install NetworkManager Open vSwitch on the target machine
func InstallNetworkManagerOpenVSwitch(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	switch distro.PackageManager {
	case platform.DNF:
		if distro.Architecture == platform.X86_64 {
			_ = cmdExec.RunCmd("sudo", "subscription-manager", "repos", "--enable=openstack-17-for-rhel-9-x86_64-rpms")
		} else {
			return platform.UnsupportedArchitecture(distro)
		}
		if err := cmdExec.RunCmd("sudo", platform.DNF, "install", "-y", "NetworkManager-ovs"); err != nil {
			return fmt.Errorf("failed to install NetworkManager-ovs: %w", err)
		}
	case platform.APT:
		// Already part of NetworkManager package
	default:
		return platform.UnsupportedPackageManager(distro)
	}
	if err := cmdExec.RunCmd("sudo", "systemctl", "enable", "openvswitch"); err != nil {
		return fmt.Errorf("failed to enable openvswitch: %w", err)
	}
	if err := cmdExec.RunCmd("sudo", "systemctl", "restart", "NetworkManager"); err != nil {
		return fmt.Errorf("failed to restart NetworkManager: %w", err)
	}
	if err := cmdExec.RunCmd("sudo", "systemctl", "start", "openvswitch"); err != nil {
		return fmt.Errorf("failed to start openvswitch: %w", err)
	}
	return nil
}

// Install kubeadm, kubelet, kubectl (kubernetes tools) on the machine
//
// From https://kubernetes.io/docs/setup/production-environment/tools/kubeadm/install-kubeadm/#installing-kubeadm-kubelet-and-kubectl
// And https://kubernetes.io/docs/setup/production-environment/container-runtimes/#installing-cri-o
func InstallKubelet(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	k8sVersion := cfg.Kubernetes.Version
	switch distro.PackageManager {
	case platform.DNF:
		// Add Kubernetes repository
		repoContent := strings.Builder{}
		repoContent.WriteString("[kubernetes]\n")
		repoContent.WriteString("name=Kubernetes\n")
		repoContent.WriteString(fmt.Sprintf("baseurl=https://pkgs.k8s.io/core:/stable:/v%s/rpm/\n", k8sVersion))
		repoContent.WriteString("enabled=1\n")
		repoContent.WriteString("gpgcheck=1\n")
		repoContent.WriteString(fmt.Sprintf("gpgkey=https://pkgs.k8s.io/core:/stable:/v%s/rpm/repodata/repomd.xml.key\n", k8sVersion))
		repoContent.WriteString("exclude=kubelet kubeadm kubectl cri-tools kubernetes-cni\n")
		if err := cmdExec.WriteFile("/etc/yum.repos.d/kubernetes.repo", []byte(repoContent.String()), 0644); err != nil {
			return fmt.Errorf("failed to write repo file: %w", err)
		}
		// Install kubelet, kubeadm, kubectl
		if err := cmdExec.RunCmd("sudo", platform.DNF, "install", "-y", "kubelet", "kubeadm", "kubectl", "--setopt=disable_excludes=kubernetes"); err != nil {
			return fmt.Errorf("failed to install kubelet, kubeadm, kubectl: %w", err)
		}
	default:
		return platform.UnsupportedPackageManager(distro)
	}
	if err := cmdExec.RunCmd("sudo", "systemctl", "enable", "kubelet"); err != nil {
		return fmt.Errorf("failed to enable kubelet: %w", err)
	}
	return nil
}

// Disable firewall on the targetmachine
func DisableFirewall(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	fmt.Printf("Disabling firewall on %s...\n", cmdExec.String())

	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	switch distro.PackageManager {
	case platform.DNF:
		// Check if firewalld is installed before trying to disable/remove it
		sb.WriteString("if rpm -q firewalld &>/dev/null; then\n")
		sb.WriteString("  sudo systemctl disable --now firewalld\n")
		sb.WriteString(fmt.Sprintf("  sudo %s remove -y firewalld\n", platform.DNF))
		sb.WriteString("fi\n")
	default:
		return platform.UnsupportedPackageManager(distro)
	}

	stdout, stderr, err := cmdExec.Execute(sb.String())
	if err != nil {
		return fmt.Errorf("failed to configure firewall: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}
	return nil
}

func CheckFirewallDisabled(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	switch distro.PackageManager {
	case platform.DNF:
		stdout, stderr, err := cmdExec.Execute("systemctl is-active firewalld")
		if strings.TrimSpace(stdout) != "inactive" {
			return fmt.Errorf("firewall is not disabled: stdout: %s, stderr: %s, err: %w", stdout, stderr, err)
		}
	default:
		return platform.UnsupportedPackageManager(distro)
	}
	return nil
}

// InstallJinjanator installs jinjanator via pip3 on the target machine
func InstallJinjanator(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	if err := cmdExec.RunCmd("pip3", "install", "--user", "jinjanator[yaml]"); err != nil {
		return fmt.Errorf("failed to install jinjanator: %w", err)
	}
	return nil
}

// InstallKubectl installs kubectl by adding the Kubernetes repository on the target machine
func InstallKubectl(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	k8sVersion := cfg.Kubernetes.Version
	switch distro.PackageManager {
	case platform.DNF:
		// Add Kubernetes repository for Fedora/RHEL/CentOS
		repoContent := strings.Builder{}
		repoContent.WriteString("[kubernetes]\n")
		repoContent.WriteString("name=Kubernetes\n")
		repoContent.WriteString(fmt.Sprintf("baseurl=https://pkgs.k8s.io/core:/stable:/v%s/rpm/\n", k8sVersion))
		repoContent.WriteString("enabled=1\n")
		repoContent.WriteString("gpgcheck=1\n")
		repoContent.WriteString(fmt.Sprintf("gpgkey=https://pkgs.k8s.io/core:/stable:/v%s/rpm/repodata/repomd.xml.key\n", k8sVersion))
		if err := cmdExec.WriteFile("/etc/yum.repos.d/kubernetes.repo", []byte(repoContent.String()), 0644); err != nil {
			return fmt.Errorf("failed to write repo file: %w", err)
		}
		// Install kubectl
		if err := cmdExec.RunCmd("sudo", platform.DNF, "install", "-y", "kubectl"); err != nil {
			return fmt.Errorf("failed to install kubectl: %w", err)
		}
	default:
		return platform.UnsupportedPackageManager(distro)
	}
	return nil
}

// InstallContainerRuntime installs Docker on the target machine
func InstallContainerRuntime(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	switch distro.PackageManager {
	case platform.DNF:
		if err := cmdExec.RunCmd("sudo", platform.DNF, "install", "-y", "podman"); err != nil {
			return fmt.Errorf("failed to install podman: %w", err)
		}
		if err := cmdExec.RunCmd("sudo", platform.DNF, "install", "-y", "docker"); err != nil {
			return fmt.Errorf("failed to install docker: %w", err)
		}
		if err := cmdExec.RunCmd("sudo", "systemctl", "start", "podman"); err != nil {
			return fmt.Errorf("failed to start podman: %w", err)
		}
		if err := cmdExec.RunCmd("sudo", "systemctl", "enable", "podman"); err != nil {
			return fmt.Errorf("failed to enable podman: %w", err)
		}
	default:
		return platform.UnsupportedPackageManager(distro)
	}
	return nil
}

// InstallKind installs Kind (Kubernetes in Docker) on the target machine
func InstallKind(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	switch distro.Architecture {
	case platform.X86_64:
		if err := cmdExec.RunCmd("curl", "-Lo", "./kind", "https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64"); err != nil {
			return fmt.Errorf("failed to download kind: %w", err)
		}
	case platform.AARCH64:
		if err := cmdExec.RunCmd("curl", "-Lo", "./kind", "https://kind.sigs.k8s.io/dl/latest/kind-linux-arm64"); err != nil {
			return fmt.Errorf("failed to download kind: %w", err)
		}
	default:
		return platform.UnsupportedArchitecture(distro)
	}

	if err := cmdExec.RunCmd("chmod", "+x", "./kind"); err != nil {
		return fmt.Errorf("failed to chmod kind: %w", err)
	}
	if err := cmdExec.RunCmd("sudo", "mv", "./kind", "/usr/local/bin/kind"); err != nil {
		return fmt.Errorf("failed to move kind to /usr/local/bin: %w", err)
	}
	return nil
}
