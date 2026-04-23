package linux

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

// Sets the hostname on the target machine
func SetHostname(cmdExec platform.CommandExecutor, hostname string) error {
	log.Debug("Setting hostname to %s on %s...", hostname, cmdExec.String())

	script := fmt.Sprintf("sudo hostnamectl set-hostname %s", hostname)

	stdout, stderr, err := cmdExec.ExecuteWithTimeout(script, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to set hostname: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	log.Info("✓ Hostname set to %s", hostname)
	return nil
}

// InstallGenericPackage installs a package dependency using the package manager
func InstallGenericPackage(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	switch distro.PackageManager {
	case platform.DNF:
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.DNF, "install", "-y", dep.Name); err != nil {
			return fmt.Errorf("failed to install %s: %w", dep.Name, err)
		}
	case platform.APT:
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.APT, "install", "-y", dep.Name); err != nil {
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
		if err := cmdExec.RunCmd(log.LevelDebug, "rpm", "-q", dep.Name); err != nil {
			return fmt.Errorf("package %s is not installed: %w", dep.Name, err)
		}
	case platform.APT:
		if err := cmdExec.RunCmd(log.LevelDebug, "dpkg", "-s", dep.Name); err != nil {
			return fmt.Errorf("package %s is not installed: %w", dep.Name, err)
		}
	default:
		return platform.UnsupportedPackageManager(distro)
	}
	return nil
}

// InstallQEMUKVM installs host QEMU virtualization packages.
// Package names differ between Fedora/RHEL and Debian/Ubuntu.
func InstallQEMUKVM(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	switch distro.PackageManager {
	case platform.DNF:
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.DNF, "install", "-y", "qemu-kvm"); err != nil {
			return fmt.Errorf("failed to install qemu-kvm: %w", err)
		}
	case platform.APT:
		pkgs := []string{"qemu-utils"}
		switch distro.Architecture {
		case platform.AARCH64:
			pkgs = append(pkgs, "qemu-system-arm")
		default:
			pkgs = append(pkgs, "qemu-system-x86")
		}
		args := append([]string{"apt", "install", "-y"}, pkgs...)
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", args...); err != nil {
			return fmt.Errorf("failed to install qemu system packages: %w", err)
		}
	default:
		return platform.UnsupportedPackageManager(distro)
	}

	return nil
}

// CheckQEMUKVM validates that a usable QEMU system emulator is present.
func CheckQEMUKVM(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	switch distro.PackageManager {
	case platform.DNF:
		return CheckGenericPackage(cmdExec, distro, cfg, dep)
	case platform.APT:
		candidates := []string{"qemu-kvm"}
		switch distro.Architecture {
		case platform.AARCH64:
			candidates = append(candidates, "qemu-system-aarch64")
		default:
			candidates = append(candidates, "qemu-system-x86_64")
		}

		for _, candidate := range candidates {
			if _, _, err := cmdExec.Execute("command -v " + candidate); err == nil {
				return nil
			}
		}

		return fmt.Errorf("no suitable qemu system binary found (checked: %s)", strings.Join(candidates, ", "))
	default:
		return platform.UnsupportedPackageManager(distro)
	}
}

// InstallAarch64UEFIFirmware installs UEFI firmware required for aarch64 VM mode hosts.
func InstallAarch64UEFIFirmware(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	if distro.Architecture != platform.AARCH64 {
		return nil
	}

	switch distro.PackageManager {
	case platform.DNF:
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.DNF, "install", "-y", "edk2-aarch64"); err != nil {
			return fmt.Errorf("failed to install edk2-aarch64: %w", err)
		}
	case platform.APT:
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.APT, "install", "-y", "qemu-efi-aarch64"); err != nil {
			return fmt.Errorf("failed to install qemu-efi-aarch64: %w", err)
		}
	default:
		return platform.UnsupportedPackageManager(distro)
	}

	return nil
}

// CheckAarch64UEFIFirmware validates that required aarch64 UEFI firmware files are present.
func CheckAarch64UEFIFirmware(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	if distro.Architecture != platform.AARCH64 {
		return nil
	}

	candidates := [][2]string{
		{"/usr/share/AAVMF/AAVMF_CODE.fd", "/usr/share/AAVMF/AAVMF_VARS.fd"},
		{"/usr/share/edk2/aarch64/QEMU_EFI.fd", "/usr/share/edk2/aarch64/QEMU_VARS.fd"},
		{"/usr/share/edk2/aarch64/QEMU_EFI-pflash.raw", "/usr/share/edk2/aarch64/vars-template-pflash.raw"},
		{"/usr/share/edk2/aarch64/QEMU_EFI-pflash.qcow2", "/usr/share/edk2/aarch64/vars-template-pflash.qcow2"},
	}

	for _, pair := range candidates {
		loaderExists, err := cmdExec.FileExists(pair[0])
		if err != nil {
			return fmt.Errorf("failed to check firmware loader path %s: %w", pair[0], err)
		}
		varsExists, err := cmdExec.FileExists(pair[1])
		if err != nil {
			return fmt.Errorf("failed to check firmware vars path %s: %w", pair[1], err)
		}
		if loaderExists && varsExists {
			return nil
		}
	}

	return fmt.Errorf("missing aarch64 UEFI firmware files: expected AAVMF/QEMU_EFI code+vars pair")
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
	if err := cmdExec.WriteFile("/etc/modules-load.d/k8s.conf", []byte(loadModulesContent.String()), 0o644); err != nil {
		return fmt.Errorf("failed to write modules load file: %w", err)
	}
	// Load kernel modules
	if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "modprobe", "overlay"); err != nil {
		return fmt.Errorf("failed to modprobe overlay: %w", err)
	}
	if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "modprobe", "br_netfilter"); err != nil {
		return fmt.Errorf("failed to modprobe br_netfilter: %w", err)
	}
	// Enable IPv4 packets to be routed between interfaces
	sysctlContent := strings.Builder{}
	sysctlContent.WriteString("net.bridge.bridge-nf-call-iptables = 1\n")
	sysctlContent.WriteString("net.bridge.bridge-nf-call-ip6tables = 1\n")
	sysctlContent.WriteString("net.ipv4.ip_forward = 1\n")
	if err := cmdExec.WriteFile("/etc/sysctl.d/k8s.conf", []byte(sysctlContent.String()), 0o644); err != nil {
		return fmt.Errorf("failed to write sysctl file: %w", err)
	}
	// Apply sysctl params without reboot
	if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "sysctl", "--system"); err != nil {
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
		if err := cmdExec.WriteFile("/etc/yum.repos.d/cri-o.repo", []byte(repoContent.String()), 0o644); err != nil {
			return fmt.Errorf("failed to write cri-o repo file: %w", err)
		}
		// Install CRI-O, iproute-tc, and containernetworking-plugins (standard CNI plugins like bridge, host-local, etc.)
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.DNF, "install", "-y", "cri-o", "iproute-tc", "containernetworking-plugins"); err != nil {
			return fmt.Errorf("failed to install CRI-O: %w", err)
		}
		// On Fedora, CNI plugins are installed to /usr/libexec/cni/. Mirror them into
		// /var/lib/cni/bin and (when writable) into /opt/cni/bin with real copies so
		// Multus thick's hostPath mount resolves delegates; pin CRI-O plugin_dirs order.
		if err := EnsureCRIOCNIPluginPaths(cmdExec); err != nil {
			return err
		}
	} else {
		return platform.UnsupportedPackageManager(distro)
	}
	if err := ConfigureCRIOLocalRegistry(cmdExec, cfg); err != nil {
		return err
	}

	if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "systemctl", "enable", "crio"); err != nil {
		return fmt.Errorf("failed to enable CRI-O: %w", err)
	}
	if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "systemctl", "start", "crio"); err != nil {
		return fmt.Errorf("failed to start CRI-O: %w", err)
	}
	return nil
}

// EnsureCRIOCNIPluginPaths ensures CRI-O can always resolve required CNI plugins
// from a consistent, writable-first path ordering across nodes.
func EnsureCRIOCNIPluginPaths(cmdExec platform.CommandExecutor) error {
	if _, stderr, err := cmdExec.ExecuteWithTimeout(`set -e
SRC=""
if [ -d /usr/libexec/cni ] && [ -n "$(ls -A /usr/libexec/cni 2>/dev/null)" ]; then
  SRC="/usr/libexec/cni"
elif [ -d /opt/cni/bin ] && [ -n "$(ls -A /opt/cni/bin 2>/dev/null)" ]; then
  SRC="/opt/cni/bin"
fi

if [ -z "$SRC" ]; then
  echo "missing CNI plugins under /usr/libexec/cni and /opt/cni/bin" >&2
  exit 1
fi

sudo mkdir -p /var/lib/cni/bin
for plugin in "$SRC"/*; do
  [ -e "$plugin" ] || continue
  sudo cp -f "$plugin" "/var/lib/cni/bin/$(basename "$plugin")"
done

sudo mkdir -p /etc/crio/crio.conf.d
sudo tee /etc/crio/crio.conf.d/99-dpu-sim-cni-plugin-dirs.conf >/dev/null <<'EOF'
[crio.network]
plugin_dirs = [
  "/var/lib/cni/bin",
  "/opt/cni/bin",
  "/usr/libexec/cni",
]
EOF

# Prevent CRI-O default bridge config from racing as primary CNI.
if [ -f /etc/cni/net.d/100-crio-bridge.conflist ]; then
  sudo mv /etc/cni/net.d/100-crio-bridge.conflist /etc/cni/net.d/100-crio-bridge.conflist.disabled
fi

# Clear stale bridge/IPAM state that can pin cni0 to the wrong CIDR.
sudo ip link delete cni0 2>/dev/null || true
sudo rm -rf /var/lib/cni/networks/crio /var/lib/cni/networks/cbr0

if [ "$SRC" = "/usr/libexec/cni" ] && sudo mkdir -p /opt/cni/bin 2>/dev/null; then
  if sudo sh -c 'touch /opt/cni/bin/.dpu-sim-write-test' 2>/dev/null; then
    sudo rm -f /opt/cni/bin/.dpu-sim-write-test
    for plugin in /usr/libexec/cni/*; do
      [ -e "$plugin" ] || continue
      sudo cp -f "$plugin" "/opt/cni/bin/$(basename "$plugin")"
    done
  else
    echo "info: /opt/cni/bin not writable; skipping mirror and copy step" >&2
  fi
fi`, 1*time.Minute); err != nil {
		return fmt.Errorf("failed to ensure CNI plugin paths: %w, stderr: %s", err, stderr)
	}
	return nil
}

// ConfigureCRIOLocalRegistry configures CRI-O to use the local dpu-sim
// registry over HTTP from cluster nodes.
func ConfigureCRIOLocalRegistry(cmdExec platform.CommandExecutor, cfg *config.Config) error {
	const registryConfPath = "/etc/containers/registries.conf.d/dpu-sim-registry.conf"

	// No-op when no local registry is configured.
	if !cfg.IsRegistryEnabled() {
		// Remove stale drop-ins from prior runs so toggling registry.enabled=false
		// actually stops CRI-O from trusting old insecure registry endpoints.
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "rm", "-f", registryConfPath); err != nil {
			return fmt.Errorf("failed to remove insecure registry config: %w", err)
		}
		return nil
	}

	endpoints := cfg.GetRegistryInsecureEndpoints()
	var blocks []string
	for _, endpoint := range endpoints {
		blocks = append(blocks, fmt.Sprintf("[[registry]]\nlocation = \"%s\"\ninsecure = true\n", endpoint))
	}
	registryConf := strings.Join(blocks, "\n")
	if err := cmdExec.WriteFile(registryConfPath, []byte(registryConf), 0o644); err != nil {
		return fmt.Errorf("failed to write insecure registry config: %w", err)
	}

	return nil
}

func enableRHELOVSRepos(cmdExec platform.CommandExecutor, distro *platform.Distro) error {
	if distro.Architecture != platform.X86_64 && distro.Architecture != platform.AARCH64 {
		return platform.UnsupportedArchitecture(distro)
	}

	if !distro.IsRHEL() {
		// Skip if not RHEL
		return nil
	}

	arch := string(distro.Architecture)
	repos := []string{
		fmt.Sprintf("codeready-builder-for-rhel-9-%s-rpms", arch),
		fmt.Sprintf("fast-datapath-for-rhel-9-%s-rpms", arch),
		fmt.Sprintf("openstack-17-for-rhel-9-%s-rpms", arch),
	}

	for _, repo := range repos {
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "subscription-manager", "repos", "--enable="+repo); err != nil {
			log.Warn("could not enable %s: %v", repo, err)
		}
	}
	return nil
}

// installOpenVSwitchPackages installs OVS packages for the distro.
// On DNF, includeNetworkManagerOvs installs NetworkManager-ovs in the same transaction as
// openvswitch so RPM scripts do not reconfigure OVS while ovsdb-server is already running
// (which can leave ovsdb-server in a failed/restart-throttled state).
func installOpenVSwitchPackages(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, _ *platform.Dependency, includeNetworkManagerOvs bool) error {
	switch distro.PackageManager {
	case platform.DNF:
		if err := enableRHELOVSRepos(cmdExec, distro); err != nil {
			return fmt.Errorf("failed to enable RHEL OVS repos: %w", err)
		}
		dnfArgs := []string{platform.DNF, "install", "-y", "openvswitch"}
		if includeNetworkManagerOvs {
			dnfArgs = append(dnfArgs, "NetworkManager-ovs")
		}
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", dnfArgs...); err != nil {
			return fmt.Errorf("failed to install openvswitch: %w", err)
		}
	case platform.APT:
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.APT, "update"); err != nil {
			return fmt.Errorf("failed to update apt: %w", err)
		}
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.APT, "install", "-y", "openvswitch-switch"); err != nil {
			return fmt.Errorf("failed to install openvswitch: %w", err)
		}
	default:
		return platform.UnsupportedPackageManager(distro)
	}
	return nil
}

func InstallOpenVSwitch(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	return installOpenVSwitchPackages(cmdExec, distro, cfg, dep, false)
}

// reviveOpenVSwitchSystemd clears systemd failure counters from crash loops, then restarts
// openvswitch so ovsdb-server and ovs-vswitchd come up cleanly after package or NM changes.
func reviveOpenVSwitchSystemd(cmdExec platform.CommandExecutor) error {
	_, _, _ = cmdExec.Execute(`sudo systemctl reset-failed ovsdb-server ovs-vswitchd openvswitch 2>/dev/null || true`)
	if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "systemctl", "restart", "openvswitch"); err != nil {
		return fmt.Errorf("failed to restart openvswitch: %w", err)
	}
	return nil
}

// isSystemdAvailable returns true if systemd is the active init system.
func isSystemdAvailable(cmdExec platform.CommandExecutor) bool {
	_, _, err := cmdExec.Execute("systemctl is-system-running")
	return err == nil
}

// Install Open vSwitch on the target machine.
//
// Behaviour varies by environment:
//   - No systemd (containers, some CI): start OVS directly via ovs-ctl.
//   - APT (Debian/Ubuntu) + systemd: apt auto-starts and enables the service on
//     install, so no explicit systemctl calls are needed.
//   - DNF (RHEL/Fedora) + systemd: rpm installs do not auto-start services;
//     enable and start openvswitch manually and restart NetworkManager.
func InstallSystemdOpenVSwitch(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	includeNM := distro.PackageManager == platform.DNF
	if err := installOpenVSwitchPackages(cmdExec, distro, cfg, dep, includeNM); err != nil {
		return fmt.Errorf("failed to install openvswitch: %w", err)
	}
	if !isSystemdAvailable(cmdExec) {
		log.Info("systemd not available, starting OVS via ovs-ctl")
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "/usr/share/openvswitch/scripts/ovs-ctl", "start", "--system-id=random"); err != nil {
			return fmt.Errorf("failed to start OVS via ovs-ctl: %w", err)
		}
		return nil
	}
	if distro.PackageManager == platform.APT {
		// Debian/Ubuntu: apt already started and enabled the service.
		return nil
	}
	// RHEL/Fedora: manually enable the service, restart NetworkManager, then restart OVS.
	if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "systemctl", "enable", "openvswitch"); err != nil {
		return fmt.Errorf("failed to enable openvswitch: %w", err)
	}
	if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "systemctl", "restart", "NetworkManager"); err != nil {
		return fmt.Errorf("failed to restart NetworkManager: %w", err)
	}
	if err := reviveOpenVSwitchSystemd(cmdExec); err != nil {
		return fmt.Errorf("failed to start openvswitch: %w", err)
	}
	return nil
}

// InstallOpenVSwitchDirect installs OVS and starts it directly with ovs-ctl.
// Use this for environments without full systemd init (e.g. Kind containers).
func InstallOpenVSwitchWithoutSystemd(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	if err := InstallOpenVSwitch(cmdExec, distro, cfg, dep); err != nil {
		return fmt.Errorf("failed to install openvswitch: %w", err)
	}
	if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "/usr/share/openvswitch/scripts/ovs-ctl", "start", "--system-id=random"); err != nil {
		return fmt.Errorf("failed to start OVS via ovs-ctl: %w", err)
	}
	return nil
}

// Install NetworkManager Open vSwitch on the target machine
func InstallNetworkManagerOpenVSwitch(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	switch distro.PackageManager {
	case platform.DNF:
		if err := enableRHELOVSRepos(cmdExec, distro); err != nil {
			return fmt.Errorf("failed to enable RHEL OVS repos: %w", err)
		}

		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.DNF, "install", "-y", "NetworkManager-ovs"); err != nil {
			return fmt.Errorf("failed to install NetworkManager-ovs: %w", err)
		}
	case platform.APT:
		// Already part of NetworkManager package
	default:
		return platform.UnsupportedPackageManager(distro)
	}
	if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "systemctl", "enable", "openvswitch"); err != nil {
		return fmt.Errorf("failed to enable openvswitch: %w", err)
	}
	if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "systemctl", "restart", "NetworkManager"); err != nil {
		return fmt.Errorf("failed to restart NetworkManager: %w", err)
	}
	if err := reviveOpenVSwitchSystemd(cmdExec); err != nil {
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
		if err := cmdExec.WriteFile("/etc/yum.repos.d/kubernetes.repo", []byte(repoContent.String()), 0o644); err != nil {
			return fmt.Errorf("failed to write repo file: %w", err)
		}
		// Install kubelet, kubeadm, kubectl
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.DNF, "install", "-y", "kubelet", "kubeadm", "kubectl", "--setopt=disable_excludes=kubernetes"); err != nil {
			return fmt.Errorf("failed to install kubelet, kubeadm, kubectl: %w", err)
		}
	default:
		return platform.UnsupportedPackageManager(distro)
	}
	if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "systemctl", "enable", "kubelet"); err != nil {
		return fmt.Errorf("failed to enable kubelet: %w", err)
	}
	return nil
}

// Disable firewall on the targetmachine
func DisableFirewall(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
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

// InstallJinjanator installs jinjanator via pip3 on the target machine.
func InstallJinjanator(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	installScript := strings.Builder{}
	installScript.WriteString("set -e\n")
	installScript.WriteString("if pip3 install --user 'jinjanator[yaml]'; then\n")
	installScript.WriteString("  exit 0\n")
	installScript.WriteString("fi\n")
	installScript.WriteString("pip3 install --user --break-system-packages 'jinjanator[yaml]'\n")

	stdout, stderr, err := cmdExec.Execute(installScript.String())
	if err != nil {
		return fmt.Errorf("failed to install jinjanator: %w, stdout: %s, stderr: %s", err, stdout, stderr)
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
		if err := cmdExec.WriteFile("/etc/yum.repos.d/kubernetes.repo", []byte(repoContent.String()), 0o644); err != nil {
			return fmt.Errorf("failed to write repo file: %w", err)
		}
		// Install kubectl
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.DNF, "install", "-y", "kubectl"); err != nil {
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
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.DNF, "install", "-y", "podman"); err != nil {
			return fmt.Errorf("failed to install podman: %w", err)
		}
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.DNF, "install", "-y", "docker"); err != nil {
			return fmt.Errorf("failed to install docker: %w", err)
		}
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "systemctl", "start", "podman"); err != nil {
			return fmt.Errorf("failed to start podman: %w", err)
		}
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "systemctl", "enable", "podman"); err != nil {
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
		if err := cmdExec.RunCmd(log.LevelDebug, "curl", "-Lo", "./kind", "https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64"); err != nil {
			return fmt.Errorf("failed to download kind: %w", err)
		}
	case platform.AARCH64:
		if err := cmdExec.RunCmd(log.LevelDebug, "curl", "-Lo", "./kind", "https://kind.sigs.k8s.io/dl/latest/kind-linux-arm64"); err != nil {
			return fmt.Errorf("failed to download kind: %w", err)
		}
	default:
		return platform.UnsupportedArchitecture(distro)
	}

	if err := cmdExec.RunCmd(log.LevelDebug, "chmod", "+x", "./kind"); err != nil {
		return fmt.Errorf("failed to chmod kind: %w", err)
	}
	if err := cmdExec.RunCmd(log.LevelDebug, "sudo", "mv", "./kind", "/usr/local/bin/kind"); err != nil {
		return fmt.Errorf("failed to move kind to /usr/local/bin: %w", err)
	}
	return nil
}

func ConfigureIpv6(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	if err := cmdExec.RunCmd(log.LevelDebug, "sysctl", "-w", "net.ipv6.conf.all.disable_ipv6=0"); err != nil {
		return fmt.Errorf("failed to configure ipv6: %w", err)
	}
	if err := cmdExec.RunCmd(log.LevelDebug, "sysctl", "-w", "net.ipv6.conf.all.forwarding=1"); err != nil {
		return fmt.Errorf("failed to configure ipv6: %w", err)
	}
	return nil
}

func CheckIpv6(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	stdout, stderr, err := cmdExec.Execute("sysctl -n net.ipv6.conf.all.disable_ipv6")
	if strings.TrimSpace(stdout) != "0" {
		return fmt.Errorf("ipv6 is not disabled: stdout: %s, stderr: %s, err: %w", stdout, stderr, err)
	}
	stdout, stderr, err = cmdExec.Execute("sysctl -n net.ipv6.conf.all.forwarding")
	if strings.TrimSpace(stdout) != "1" {
		return fmt.Errorf("ipv6 forwarding is not enabled: stdout: %s, stderr: %s, err: %w", stdout, stderr, err)
	}
	return nil
}

// InstallKindCNIPlugins installs and stages the base CNI binaries flannel and
// multus depend on for Kind node networking.
//
// Why: on some Debian-based Kind node images, /opt/cni/bin only has a subset
// of plugins after multus install. Flannel then fails to delegate to "bridge"
// and pods stay in ContainerCreating/Pending.
//
// What: install containernetworking-plugins and copy key binaries into
// /opt/cni/bin (bridge, host-local, loopback, portmap, ptp).
//
// How: detect the distro plugin source path (/usr/lib/cni or
// /usr/libexec/cni), then copy only binaries that exist so the routine is safe
// across distro packaging differences.
func InstallKindCNIPlugins(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	switch distro.PackageManager {
	case platform.APT:
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.APT, "update"); err != nil {
			return fmt.Errorf("failed to update apt: %w", err)
		}
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.APT, "install", "-y", "containernetworking-plugins"); err != nil {
			return fmt.Errorf("failed to install containernetworking-plugins: %w", err)
		}
	case platform.DNF:
		if err := cmdExec.RunCmd(log.LevelDebug, "sudo", platform.DNF, "install", "-y", "containernetworking-plugins"); err != nil {
			return fmt.Errorf("failed to install containernetworking-plugins: %w", err)
		}
	default:
		return platform.UnsupportedPackageManager(distro)
	}

	stdout, stderr, err := cmdExec.Execute(`set -e
SRC=""
for d in /usr/lib/cni /usr/libexec/cni; do
  if [ -x "$d/bridge" ]; then
    SRC="$d"
    break
  fi
done

if [ -z "$SRC" ]; then
  echo "missing bridge plugin under /usr/lib/cni or /usr/libexec/cni" >&2
  exit 1
fi

sudo mkdir -p /opt/cni/bin
for plugin in bridge host-local loopback portmap ptp; do
  if [ -x "$SRC/$plugin" ]; then
    sudo cp -f "$SRC/$plugin" "/opt/cni/bin/$plugin"
  fi
done`)
	if err != nil {
		return fmt.Errorf("failed to place kind cni plugins in /opt/cni/bin: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	return nil
}

// CheckKindCNIPlugins validates a minimal set that proves bridge-style primary networking can run.
// bridge + host-local are the critical pair required by flannel delegation.
func CheckKindCNIPlugins(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	stdout, stderr, err := cmdExec.Execute("test -x /opt/cni/bin/bridge && test -x /opt/cni/bin/host-local")
	if err != nil {
		return fmt.Errorf("required cni plugins are missing from /opt/cni/bin: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}
	return nil
}

const (
	MinInotifyMaxUserInstances = 8192
	MinInotifyMaxUserWatches   = 1048576
	MinInotifyMaxQueuedEvents  = 32768
)

func ConfigureInotifyLimits(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	sb.WriteString("sudo mkdir -p /etc/sysctl.d\n")
	sb.WriteString(fmt.Sprintf("echo 'fs.inotify.max_user_instances=%d' | sudo tee /etc/sysctl.d/99-dpu-sim-kind.conf >/dev/null\n", MinInotifyMaxUserInstances))
	sb.WriteString(fmt.Sprintf("echo 'fs.inotify.max_user_watches=%d' | sudo tee -a /etc/sysctl.d/99-dpu-sim-kind.conf >/dev/null\n", MinInotifyMaxUserWatches))
	sb.WriteString(fmt.Sprintf("echo 'fs.inotify.max_queued_events=%d' | sudo tee -a /etc/sysctl.d/99-dpu-sim-kind.conf >/dev/null\n", MinInotifyMaxQueuedEvents))
	sb.WriteString(fmt.Sprintf("sudo sysctl -w fs.inotify.max_user_instances=%d\n", MinInotifyMaxUserInstances))
	sb.WriteString(fmt.Sprintf("sudo sysctl -w fs.inotify.max_user_watches=%d\n", MinInotifyMaxUserWatches))
	sb.WriteString(fmt.Sprintf("sudo sysctl -w fs.inotify.max_queued_events=%d\n", MinInotifyMaxQueuedEvents))

	stdout, stderr, err := cmdExec.ExecuteWithTimeout(sb.String(), 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to configure inotify limits: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	return nil
}

// CheckBrNetfilter verifies the br_netfilter module is loaded and the
// bridge-nf-call-iptables sysctl is enabled. Kind nodes share the host
// kernel, so this must be satisfied on the host for flannel to work inside
// the containers.
func CheckBrNetfilter(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	stdout, stderr, err := cmdExec.Execute("lsmod | grep br_netfilter")
	if err != nil {
		return fmt.Errorf("failed to check br_netfilter module: %w, stderr: %s", err, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		return fmt.Errorf("br_netfilter kernel module is not loaded")
	}

	stdout, stderr, err = cmdExec.Execute("sysctl -n net.bridge.bridge-nf-call-iptables")
	if err != nil {
		return fmt.Errorf("failed to read net.bridge.bridge-nf-call-iptables: %w, stderr: %s", err, stderr)
	}
	if strings.TrimSpace(stdout) != "1" {
		return fmt.Errorf("net.bridge.bridge-nf-call-iptables is not enabled")
	}
	return nil
}

// ConfigureBrNetfilter loads the br_netfilter module and enables the
// bridge-nf-call-iptables sysctl on the host.
func ConfigureBrNetfilter(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	sb := strings.Builder{}
	sb.WriteString("set -e\n")
	sb.WriteString("sudo modprobe br_netfilter\n")
	sb.WriteString("echo 'br_netfilter' | sudo tee /etc/modules-load.d/br_netfilter.conf >/dev/null\n")
	sb.WriteString("sudo sysctl -w net.bridge.bridge-nf-call-iptables=1\n")
	sb.WriteString("sudo sysctl -w net.bridge.bridge-nf-call-ip6tables=1\n")

	stdout, stderr, err := cmdExec.ExecuteWithTimeout(sb.String(), 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to configure br_netfilter: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}
	return nil
}

func CheckInotifyLimits(cmdExec platform.CommandExecutor, distro *platform.Distro, cfg *config.Config, dep *platform.Dependency) error {
	checks := []struct {
		key      string
		minValue int
	}{
		{key: "fs.inotify.max_user_instances", minValue: MinInotifyMaxUserInstances},
		{key: "fs.inotify.max_user_watches", minValue: MinInotifyMaxUserWatches},
		{key: "fs.inotify.max_queued_events", minValue: MinInotifyMaxQueuedEvents},
	}

	for _, c := range checks {
		stdout, stderr, err := cmdExec.Execute(fmt.Sprintf("sysctl -n %s", c.key))
		if err != nil {
			return fmt.Errorf("failed to read %s: %w, stderr: %s", c.key, err, stderr)
		}

		value, err := strconv.Atoi(strings.TrimSpace(stdout))
		if err != nil {
			return fmt.Errorf("failed to parse %s value %q: %w", c.key, strings.TrimSpace(stdout), err)
		}

		if value < c.minValue {
			return fmt.Errorf("%s is %d, expected >= %d", c.key, value, c.minValue)
		}
	}

	return nil
}
