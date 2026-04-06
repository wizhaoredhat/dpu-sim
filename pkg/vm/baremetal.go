package vm

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

type hostEgressInfo struct {
	iface string
	ip    string
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (m *VMManager) globalSSHExecutor(ip string) *platform.SSHExecutor {
	return platform.NewSSHExecutor(&m.config.SSH, ip)
}

func (m *VMManager) bootstrapExecutor(node config.BareMetalConfig) *platform.SSHExecutor {
	if node.BootstrapSSH == nil {
		return nil
	}
	bootstrap := *node.BootstrapSSH
	if bootstrap.User == "" {
		bootstrap.User = m.config.SSH.User
	}
	return platform.NewSSHExecutor(&bootstrap, node.MgmtIP)
}

func (m *VMManager) ensureBareMetalSSHAccess(node config.BareMetalConfig) (*platform.SSHExecutor, error) {
	globalExec := m.globalSSHExecutor(node.MgmtIP)
	if err := globalExec.WaitUntilReady(20 * time.Second); err == nil {
		return globalExec, nil
	}

	bootstrapExec := m.bootstrapExecutor(node)
	if bootstrapExec == nil {
		return nil, fmt.Errorf("failed SSH auth to baremetal node %s at %s and no bootstrap_ssh provided", node.Name, node.MgmtIP)
	}
	if err := bootstrapExec.WaitUntilReady(1 * time.Minute); err != nil {
		return nil, fmt.Errorf("failed bootstrap SSH auth to baremetal node %s at %s: %w", node.Name, node.MgmtIP, err)
	}

	if err := m.installGlobalSSHAccess(bootstrapExec); err != nil {
		return nil, fmt.Errorf("failed to install common SSH key on baremetal node %s: %w", node.Name, err)
	}

	if err := globalExec.WaitUntilReady(2 * time.Minute); err != nil {
		return nil, fmt.Errorf("failed to authenticate with common SSH key on baremetal node %s after bootstrap: %w", node.Name, err)
	}

	return globalExec, nil
}

func (m *VMManager) installGlobalSSHAccess(cmdExec platform.CommandExecutor) error {
	pubKeyPath := m.config.SSH.KeyPath + ".pub"
	pubKey, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read public key %s: %w", pubKeyPath, err)
	}

	user := m.config.SSH.User
	if user == "" {
		user = "root"
	}

	homeDir, err := resolveRemoteUserHomeDir(cmdExec, user)
	if err != nil {
		return err
	}

	quotedKey := shellQuote(strings.TrimSpace(string(pubKey)))
	quotedHome := shellQuote(homeDir)
	script := strings.Builder{}
	script.WriteString("set -e\n")
	script.WriteString(fmt.Sprintf("sudo mkdir -p %s/.ssh\n", quotedHome))
	script.WriteString(fmt.Sprintf("sudo chmod 700 %s/.ssh\n", quotedHome))
	script.WriteString(fmt.Sprintf("if ! sudo grep -qxF %s %s/.ssh/authorized_keys 2>/dev/null; then sudo sh -c \"printf '%%s\\n' %s >> %s/.ssh/authorized_keys\"; fi\n", quotedKey, quotedHome, quotedKey, quotedHome))
	script.WriteString(fmt.Sprintf("sudo chmod 600 %s/.ssh/authorized_keys\n", quotedHome))
	if user != "root" {
		script.WriteString(fmt.Sprintf("sudo chown -R %s:%s %s/.ssh\n", shellQuote(user), shellQuote(user), quotedHome))
	}

	stdout, stderr, err := cmdExec.ExecuteWithTimeout(script.String(), 1*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to install authorized key: %w, stdout: %s, stderr: %s", err, stdout, stderr)
	}

	return nil
}

// resolveRemoteUserHomeDir resolves the target user's home directory on the
// remote node.
//
// Why: baremetal targets can use non-standard home roots (e.g. /var/home on
// FCOS-like systems), so assuming /home/<user> can write authorized_keys into
// the wrong path and break bootstrap-to-global SSH handoff.
func resolveRemoteUserHomeDir(cmdExec platform.CommandExecutor, user string) (string, error) {
	if user == "root" {
		return "/root", nil
	}

	stdout, stderr, err := cmdExec.ExecuteWithTimeout(
		fmt.Sprintf("getent passwd %s | cut -d: -f6", shellQuote(user)),
		30*time.Second,
	)
	homeDir := strings.TrimSpace(stdout)
	if err == nil && homeDir != "" {
		return homeDir, nil
	}

	for _, fallback := range []string{fmt.Sprintf("/var/home/%s", user), fmt.Sprintf("/home/%s", user)} {
		exists, existsErr := cmdExec.FileExists(fallback)
		if existsErr == nil && exists {
			return fallback, nil
		}
	}

	if err != nil {
		return "", fmt.Errorf("failed to resolve home dir for %s: %w, stderr: %s", user, err, strings.TrimSpace(stderr))
	}
	return "", fmt.Errorf("resolved empty home dir for %s and no fallback home path exists", user)
}

func (m *VMManager) maybeApplyBootc(node config.BareMetalConfig, cmdExec platform.CommandExecutor) error {
	if node.Bootc == nil || !node.Bootc.Enabled {
		return nil
	}

	strategy := node.Bootc.Strategy
	if strategy == "" {
		strategy = "switch"
	}

	parts := []string{"sudo", "bootc"}
	if strategy == "upgrade" {
		parts = append(parts, "upgrade")
		if node.Bootc.Apply {
			parts = append(parts, "--apply")
			if node.Bootc.SoftReboot != "" {
				parts = append(parts, "--soft-reboot", node.Bootc.SoftReboot)
			}
		}
	} else {
		parts = append(parts, "switch")
		if node.Bootc.Apply {
			parts = append(parts, "--apply")
			if node.Bootc.SoftReboot != "" {
				parts = append(parts, "--soft-reboot", node.Bootc.SoftReboot)
			}
		}
		if node.Bootc.Transport != "" {
			parts = append(parts, "--transport", node.Bootc.Transport)
		}
		if node.Bootc.EnforceContainerSigpolicy {
			parts = append(parts, "--enforce-container-sigpolicy")
		}
		if node.Bootc.Retain {
			parts = append(parts, "--retain")
		}
		if node.Bootc.ImageRef != "" {
			parts = append(parts, node.Bootc.ImageRef)
		}
	}

	command := strings.Join(parts, " ")
	log.Info("Applying bootc %s on baremetal node %s...", strategy, node.Name)
	_, _, err := cmdExec.ExecuteWithTimeout(command, 20*time.Minute)
	if err != nil && !node.Bootc.Apply {
		return fmt.Errorf("bootc command failed on node %s: %w", node.Name, err)
	}

	if node.Bootc.Apply {
		wait := 10 * time.Minute
		if node.Bootc.WaitAfterReboot != "" {
			parsed, parseErr := time.ParseDuration(node.Bootc.WaitAfterReboot)
			if parseErr != nil {
				return fmt.Errorf("invalid bootc.wait_after_reboot for node %s: %w", node.Name, parseErr)
			}
			wait = parsed
		}
		reconnect := m.globalSSHExecutor(node.MgmtIP)
		if reconnectErr := reconnect.WaitUntilReady(wait); reconnectErr != nil {
			return fmt.Errorf("node %s did not come back after bootc apply reboot: %w", node.Name, reconnectErr)
		}
	}

	return nil
}

type resetPhase struct {
	name    string
	timeout time.Duration
	script  string
}

func buildScript(commands ...string) string {
	script := strings.Builder{}
	script.WriteString("set -e\n")
	for _, cmd := range commands {
		script.WriteString(cmd)
		script.WriteString("\n")
	}
	return script.String()
}

func buildBareMetalResetPhases() []resetPhase {
	return []resetPhase{
		{
			name:    "stop kubernetes services",
			timeout: 90 * time.Second,
			script: buildScript(
				"sudo kubeadm reset -f || true",
				"sudo systemctl stop kubelet || true",
				"sudo systemctl stop crio || true",
				"sudo systemctl stop containerd || true",
			),
		},
		{
			name:    "cleanup kubernetes state",
			timeout: 90 * time.Second,
			script: buildScript(
				"sudo rm -rf /etc/cni/net.d /var/lib/cni",
				"sudo rm -rf /etc/kubernetes /var/lib/kubelet",
				"sudo rm -f /etc/systemd/system/kubelet.service",
				"sudo rm -rf /etc/systemd/system/kubelet.service.d",
			),
		},
		{
			name:    "ensure ovs prerequisites",
			timeout: 60 * time.Second,
			script: buildScript(
				"if ! getent group openvswitch >/dev/null; then sudo groupadd -r openvswitch || true; fi",
				"if ! getent group hugetlbfs >/dev/null; then sudo groupadd -r hugetlbfs || true; fi",
				"if ! id openvswitch >/dev/null 2>&1; then sudo useradd -r -g openvswitch -s /sbin/nologin openvswitch || true; fi",
				"sudo usermod -a -G hugetlbfs openvswitch || true",
			),
		},
		{
			name:    "reload services",
			timeout: 60 * time.Second,
			script: buildScript(
				"sudo systemctl daemon-reload",
				"sudo systemctl reset-failed || true",
				"sudo systemctl restart openvswitch || true",
			),
		},
		{
			name:    "validate reset state",
			timeout: 45 * time.Second,
			script: buildScript(
				"if sudo test -e /etc/kubernetes/kubelet.conf; then echo 'kubelet.conf still present after reset'; exit 1; fi",
				"if sudo systemctl is-active --quiet kubelet; then echo 'kubelet is still active after reset'; exit 1; fi",
			),
		},
	}
}

func (m *VMManager) resetBareMetalNode(node config.BareMetalConfig, cmdExec platform.CommandExecutor) error {
	for _, phase := range buildBareMetalResetPhases() {
		log.Info("Reset baremetal node %s: %s", node.Name, phase.name)
		stdout, stderr, err := cmdExec.ExecuteWithTimeout(phase.script, phase.timeout)
		if err != nil {
			return fmt.Errorf("failed to reset baremetal node %s during phase %q: %w, stdout: %s, stderr: %s", node.Name, phase.name, err, stdout, stderr)
		}
	}

	return nil
}

func (m *VMManager) setKubeletNodeIP(node config.BareMetalConfig, cmdExec platform.CommandExecutor) error {
	distro, err := cmdExec.GetDistro()
	if err != nil {
		return fmt.Errorf("failed to detect distro for node %s: %w", node.Name, err)
	}

	kubeletEnv := "/etc/default/kubelet"
	if distro.IsFedoraLike() {
		kubeletEnv = "/etc/sysconfig/kubelet"
	}

	script := strings.Builder{}
	script.WriteString("set -e\n")
	script.WriteString("sudo mkdir -p /etc/systemd/system/kubelet.service.d\n")
	script.WriteString(fmt.Sprintf("sudo touch %s\n", shellQuote(kubeletEnv)))
	script.WriteString(fmt.Sprintf("sudo sed -i '/^KUBELET_EXTRA_ARGS=/d' %s\n", shellQuote(kubeletEnv)))
	script.WriteString(fmt.Sprintf("sudo sh -c \"printf 'KUBELET_EXTRA_ARGS=\\\"--node-ip=%s\\\"\\n' >> %s\"\n", node.NodeIP, shellQuote(kubeletEnv)))
	script.WriteString("sudo systemctl daemon-reload\n")
	script.WriteString("sudo systemctl restart kubelet || true\n")

	stdout, stderr, err := cmdExec.ExecuteWithTimeout(script.String(), 2*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to set kubelet node-ip on %s: %w, stdout: %s, stderr: %s", node.Name, err, stdout, stderr)
	}

	return nil
}

func (m *VMManager) detectHostEgressTo(ip string) (*hostEgressInfo, error) {
	local := platform.NewLocalExecutor()
	cmd := fmt.Sprintf("ip -4 route get %s | awk '{for(i=1;i<=NF;i++){if($i==\"dev\")dev=$(i+1); if($i==\"src\")src=$(i+1)}} END{print dev,src}'", shellQuote(ip))
	stdout, stderr, err := local.ExecuteWithTimeout(cmd, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to detect host egress route to %s: %w, stderr: %s", ip, err, stderr)
	}

	fields := strings.Fields(strings.TrimSpace(stdout))
	if len(fields) < 2 {
		return nil, fmt.Errorf("failed to parse host egress route to %s from output %q", ip, stdout)
	}

	return &hostEgressInfo{iface: fields[0], ip: fields[1]}, nil
}

func getRouteDevice(cmdExec platform.CommandExecutor, ip string) (string, error) {
	cmd := fmt.Sprintf("ip -4 route get %s | awk '{for(i=1;i<=NF;i++){if($i==\"dev\"){print $(i+1); exit}}}'", shellQuote(ip))
	stdout, stderr, err := cmdExec.ExecuteWithTimeout(cmd, 30*time.Second)
	if err != nil {
		return "", fmt.Errorf("failed to detect route device to %s: %w, stderr: %s", ip, err, stderr)
	}
	return strings.TrimSpace(stdout), nil
}

func ensureForwardRule(local *platform.LocalExecutor, inIface, outIface, src, dst string) error {
	script := strings.Builder{}
	script.WriteString("set -e\n")
	script.WriteString("sudo iptables -C FORWARD")
	if inIface != "" {
		script.WriteString(" -i " + shellQuote(inIface))
	}
	if outIface != "" {
		script.WriteString(" -o " + shellQuote(outIface))
	}
	if src != "" {
		script.WriteString(" -s " + shellQuote(src))
	}
	if dst != "" {
		script.WriteString(" -d " + shellQuote(dst))
	}
	script.WriteString(" -j ACCEPT || sudo iptables -I FORWARD 1")
	if inIface != "" {
		script.WriteString(" -i " + shellQuote(inIface))
	}
	if outIface != "" {
		script.WriteString(" -o " + shellQuote(outIface))
	}
	if src != "" {
		script.WriteString(" -s " + shellQuote(src))
	}
	if dst != "" {
		script.WriteString(" -d " + shellQuote(dst))
	}
	script.WriteString(" -j ACCEPT\n")

	stdout, stderr, err := local.ExecuteWithTimeout(script.String(), 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to ensure FORWARD rule in=%s out=%s src=%s dst=%s: %w, stdout: %s, stderr: %s", inIface, outIface, src, dst, err, stdout, stderr)
	}

	return nil
}

func (m *VMManager) ensureHybridNetworking(node config.BareMetalConfig, firstMasterExec platform.CommandExecutor) error {
	mgmtNet := m.config.GetNetworkByType(config.MgmtNetworkName)
	k8sNet := m.config.GetNetworkByType(config.K8sNetworkName)
	if mgmtNet == nil || k8sNet == nil {
		return fmt.Errorf("missing mgmt/k8s network definitions for baremetal integration")
	}

	mgmtSubnet := mgmtNet.GetSubnetCIDR()
	k8sSubnet := k8sNet.GetSubnetCIDR()
	if mgmtSubnet == "" || k8sSubnet == "" {
		return fmt.Errorf("failed to derive mgmt/k8s subnet CIDRs from configuration")
	}

	egress, err := m.detectHostEgressTo(node.MgmtIP)
	if err != nil {
		return err
	}

	local := platform.NewLocalExecutor()
	if _, _, err := local.ExecuteWithTimeout("sudo sysctl -w net.ipv4.ip_forward=1", 15*time.Second); err != nil {
		return fmt.Errorf("failed to enable net.ipv4.ip_forward on host: %w", err)
	}

	if err := ensureForwardRule(local, egress.iface, mgmtNet.BridgeName, "", mgmtSubnet); err != nil {
		return err
	}
	if err := ensureForwardRule(local, mgmtNet.BridgeName, egress.iface, mgmtSubnet, ""); err != nil {
		return err
	}
	if err := ensureForwardRule(local, egress.iface, k8sNet.BridgeName, "", k8sSubnet); err != nil {
		return err
	}
	if err := ensureForwardRule(local, k8sNet.BridgeName, egress.iface, k8sSubnet, ""); err != nil {
		return err
	}

	// Also allow explicit host forwarding between this baremetal node and the
	// cluster k8s subnet regardless of ingress interface naming/topology.
	// This prevents libvirt/firewalld chains from rejecting packets sourced from
	// external baremetal subnets (for example 172.22.0.0/16) destined to VM k8s
	// network addresses (for example 192.168.123.0/24).
	nodeCIDR := fmt.Sprintf("%s/32", node.MgmtIP)
	if err := ensureForwardRule(local, "", "", nodeCIDR, k8sSubnet); err != nil {
		return err
	}
	if err := ensureForwardRule(local, "", "", k8sSubnet, nodeCIDR); err != nil {
		return err
	}

	bmExec, err := m.ensureBareMetalSSHAccess(node)
	if err != nil {
		return err
	}

	routeIface := strings.TrimSpace(node.GatewayInterface)
	if routeIface != "" {
		if _, _, err := bmExec.ExecuteWithTimeout(fmt.Sprintf("ip link show %s", shellQuote(routeIface)), 15*time.Second); err != nil {
			routeIface = ""
		}
	}

	ifaceArg := ""
	if routeIface != "" {
		ifaceArg = fmt.Sprintf(" dev %s", shellQuote(routeIface))
	}
	bmScript := strings.Builder{}
	bmScript.WriteString("set -e\n")
	bmScript.WriteString(fmt.Sprintf("sudo ip route replace %s via %s%s\n", shellQuote(mgmtSubnet), shellQuote(egress.ip), ifaceArg))
	bmScript.WriteString(fmt.Sprintf("sudo ip route replace %s via %s%s\n", shellQuote(k8sSubnet), shellQuote(egress.ip), ifaceArg))

	stdout, stderr, err := bmExec.ExecuteWithTimeout(bmScript.String(), 1*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to program routes on baremetal node %s: %w, stdout: %s, stderr: %s", node.Name, err, stdout, stderr)
	}

	shouldAddMasterRoute := false
	if k8sNet.Gateway != "" {
		routeDev, err := getRouteDevice(firstMasterExec, node.MgmtIP)
		if err != nil {
			log.Warn("Could not inspect first-master route to %s, adding explicit /32 return route via %s: %v", node.MgmtIP, k8sNet.Gateway, err)
			shouldAddMasterRoute = true
		} else if routeDev == "" {
			log.Warn("First master has no detected route device to %s, adding explicit /32 return route via %s", node.MgmtIP, k8sNet.Gateway)
			shouldAddMasterRoute = true
		} else {
			log.Info("Skipping first-master /32 route for baremetal node %s because route to %s already resolves via device %s", node.Name, node.MgmtIP, routeDev)
		}
	}

	if shouldAddMasterRoute {
		masterScript := fmt.Sprintf(`set -e
DEV="brk8s"
if ! ip link show "$DEV" >/dev/null 2>&1; then
  DEV="%s"
fi
if ip link show "$DEV" >/dev/null 2>&1; then
  sudo ip route replace %s/32 via %s dev "$DEV"
else
  sudo ip route replace %s/32 via %s
fi`, config.K8sNetworkName, shellQuote(node.MgmtIP), shellQuote(k8sNet.Gateway), shellQuote(node.MgmtIP), shellQuote(k8sNet.Gateway))
		stdout, stderr, err = firstMasterExec.ExecuteWithTimeout(masterScript, 30*time.Second)
		if err != nil {
			return fmt.Errorf("failed to add return route on first master for node %s: %w, stdout: %s, stderr: %s", node.Name, err, stdout, stderr)
		}
	}

	return nil
}
