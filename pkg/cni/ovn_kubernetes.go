package cni

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

const (
	// DefaultOVNImage is the default OVN-Kubernetes image
	DefaultOVNImage = "ghcr.io/ovn-kubernetes/ovn-kubernetes/ovn-kube-fedora:master"
)

// patchCoreDNSForOVN patches the CoreDNS configmap for OVN-Kubernetes compatibility.
// This modifies CoreDNS to:
// 1. Work in an offline environment (no IPv6 connectivity)
// 2. Handle additional domains (like .net.) and return NXDOMAIN instead of SERVFAIL
// 3. Remove problematic directives like 'loop', 'upstream', and 'fallthrough' from CoreDNS config
func (m *CNIManager) patchCoreDNS(dnsServer string) error {
	log.Info("Patching CoreDNS configmap for OVN-Kubernetes compatibility, dns server: %s", dnsServer)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get the current CoreDNS configmap
	configMap, err := m.k8sClient.Clientset().CoreV1().ConfigMaps("kube-system").Get(ctx, "coredns", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get CoreDNS configmap: %w", err)
	}

	// Get the Corefile content
	corefile, ok := configMap.Data["Corefile"]
	if !ok {
		return fmt.Errorf("Corefile not found in CoreDNS configmap")
	}

	// Apply patches to the Corefile
	var patchedLines []string
	for _, line := range strings.Split(corefile, "\n") {
		// Skip lines containing 'upstream', 'fallthrough', or 'loop'
		// These are problematic lines that need to be removed
		if regexp.MustCompile(`^\s*upstream\s*$`).MatchString(line) {
			continue
		}
		if regexp.MustCompile(`^\s*fallthrough.*$`).MatchString(line) {
			continue
		}
		if regexp.MustCompile(`^\s*loop\s*$`).MatchString(line) {
			continue
		}

		// Add 'net' after 'kubernetes cluster.local' to handle .net. domain
		line = regexp.MustCompile(`^(\s*kubernetes cluster\.local)`).ReplaceAllString(line, "${1} net")

		// Replace forward line to use specified DNS server
		line = regexp.MustCompile(`^(\s*forward \.)\s*.*$`).ReplaceAllString(line, fmt.Sprintf("${1} %s {", dnsServer))

		patchedLines = append(patchedLines, line)
	}

	patchedCorefile := strings.Join(patchedLines, "\n")

	// Update the configmap with the patched Corefile
	configMap.Data["Corefile"] = patchedCorefile

	_, err = m.k8sClient.Clientset().CoreV1().ConfigMaps("kube-system").Update(ctx, configMap, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update CoreDNS configmap: %w", err)
	}

	log.Info("✓ CoreDNS configmap patched successfully")
	return nil
}

// runDaemonsetScript runs the OVN-Kubernetes daemonset.sh script to generate manifests
func (m *CNIManager) runDaemonsetScript(ovnkRepoPath, apiServerURL, podCIDR, serviceCIDR, ovnImage string) error {
	daemonsetScript := filepath.Join(ovnkRepoPath, "dist", "images", "daemonset.sh")

	if _, err := os.Stat(daemonsetScript); os.IsNotExist(err) {
		return fmt.Errorf("daemonset.sh script not found at %s", daemonsetScript)
	}

	if err := os.Chmod(daemonsetScript, 0755); err != nil {
		return fmt.Errorf("failed to make daemonset.sh executable: %w", err)
	}

	args := []string{
		fmt.Sprintf("--image=%s", ovnImage),
		fmt.Sprintf("--net-cidr=%s", podCIDR),
		fmt.Sprintf("--svc-cidr=%s", serviceCIDR),
		fmt.Sprintf("--k8s-apiserver=%s", apiServerURL),
		"--gateway-mode=shared",
		"--dummy-gateway-bridge=false",
		"--gateway-options=",
		"--enable-ipsec=false",
		"--hybrid-enabled=false",
		"--disable-snat-multiple-gws=false",
		"--disable-forwarding=false",
		"--ovn-encap-port=",
		"--disable-pkt-mtu-check=false",
		"--ovn-empty-lb-events=false",
		"--multicast-enabled=false",
		"--ovn-master-count=1",
		"--ovn-unprivileged-mode=no",
		"--master-loglevel=5",
		"--node-loglevel=5",
		"--dbchecker-loglevel=5",
		"--ovn-loglevel-northd=-vconsole:info -vfile:info",
		"--ovn-loglevel-nb=-vconsole:info -vfile:info",
		"--ovn-loglevel-sb=-vconsole:info -vfile:info",
		"--ovn-loglevel-controller=-vconsole:info",
		"--ovnkube-libovsdb-client-logfile=",
		"--ovnkube-config-duration-enable=true",
		"--admin-network-policy-enable=true",
		"--egress-ip-enable=true",
		"--egress-ip-healthcheck-port=9107",
		"--egress-firewall-enable=true",
		"--egress-qos-enable=true",
		"--egress-service-enable=true",
		"--v4-join-subnet=100.64.0.0/16",
		"--v6-join-subnet=fd98::/64",
		"--v4-masquerade-subnet=169.254.0.0/17",
		"--v6-masquerade-subnet=fd69::/112",
		"--v4-transit-subnet=100.88.0.0/16",
		"--v6-transit-subnet=fd97::/64",
		"--ex-gw-network-interface=",
		"--multi-network-enable=false",
		"--network-segmentation-enable=false",
		"--preconfigured-udn-addresses-enable=false",
		"--route-advertisements-enable=false",
		"--advertise-default-network=false",
		"--advertised-udn-isolation-mode=strict",
		"--ovnkube-metrics-scale-enable=false",
		"--compact-mode=false",
		"--enable-multi-external-gateway=true",
		"--enable-ovnkube-identity=true",
		"--enable-persistent-ips=true",
		"--network-qos-enable=false",
		"--mtu=1400",
		"--enable-dnsnameresolver=false",
		"--enable-observ=false",
	}

	log.Info("Running daemonset.sh to generate manifests...")
	cmd := exec.Command(daemonsetScript, args...)
	cmd.Dir = filepath.Dir(daemonsetScript)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("daemonset.sh failed stdout: %s, stderr: %s", output, err)
	}

	log.Info("✓ daemonset.sh completed successfully")
	log.Debug("daemonset.sh output: %s", output)
	return nil
}

// applyOVNKubernetesManifests applies the OVN-Kubernetes manifests generated by daemonset.sh
func (m *CNIManager) applyOVNKubernetesManifests(ovnPath string, ovsNode bool) error {
	yamlDir := filepath.Join(ovnPath, "dist", "yaml")

	// Check if yaml directory exists
	if _, err := os.Stat(yamlDir); os.IsNotExist(err) {
		return fmt.Errorf("YAML directory not found at %s", yamlDir)
	}

	// CRD manifests to apply first
	crdManifests := []string{
		"k8s.ovn.org_egressfirewalls.yaml",
		"k8s.ovn.org_egressips.yaml",
		"k8s.ovn.org_egressqoses.yaml",
		"k8s.ovn.org_egressservices.yaml",
		"k8s.ovn.org_adminpolicybasedexternalroutes.yaml",
		"k8s.ovn.org_networkqoses.yaml",
		"k8s.ovn.org_userdefinednetworks.yaml",
		"k8s.ovn.org_clusteruserdefinednetworks.yaml",
		"k8s.ovn.org_routeadvertisements.yaml",
		"k8s.ovn.org_clusternetworkconnects.yaml",
	}

	// External CRDs (ANP & BANP)
	externalCRDs := []string{
		"https://raw.githubusercontent.com/kubernetes-sigs/network-policy-api/v0.1.5/config/crd/experimental/policy.networking.k8s.io_adminnetworkpolicies.yaml",
		"https://raw.githubusercontent.com/kubernetes-sigs/network-policy-api/v0.1.5/config/crd/experimental/policy.networking.k8s.io_baselineadminnetworkpolicies.yaml",
	}

	// Setup and RBAC manifests
	setupManifests := []string{
		// ovn-setup.yaml creates the ovn-kubernetes namespace and RBAC resources (no pods are created)
		"ovn-setup.yaml",
		// Create all RBAC resources and service accounts
		"rbac-ovnkube-identity.yaml",
		"rbac-ovnkube-cluster-manager.yaml",
		"rbac-ovnkube-master.yaml",
		"rbac-ovnkube-node.yaml",
		"rbac-ovnkube-db.yaml",
	}

	deploymentManifests := []string{
		// ovnkube-identity.yaml creates the ovnkube-identity deployment which approves pending CSRs
		"ovnkube-identity.yaml",
	}

	if ovsNode {
		deploymentManifests = append(deploymentManifests, "ovs-node.yaml")
	}
	deploymentManifests = append(deploymentManifests, "ovnkube-db.yaml")
	deploymentManifests = append(deploymentManifests, "ovnkube-master.yaml")
	deploymentManifests = append(deploymentManifests, "ovnkube-node.yaml")

	log.Info("Applying OVN-Kubernetes CRD manifests...")
	for _, manifest := range crdManifests {
		manifestPath := filepath.Join(yamlDir, manifest)
		content, err := os.ReadFile(manifestPath)
		if err != nil {
			return fmt.Errorf("failed to read manifest %s: %w", manifest, err)
		}
		if err := m.k8sClient.ApplyManifest(content); err != nil {
			return fmt.Errorf("failed to apply manifest %s: %w", manifest, err)
		}
		log.Debug("✓ Applied CRD manifest %s", manifest)
	}

	log.Info("Applying external CRD manifests...")
	for _, url := range externalCRDs {
		if err := m.k8sClient.ApplyManifestFromURL(url); err != nil {
			return fmt.Errorf("failed to apply external CRD from %s: %w", url, err)
		}
	}

	// Invalidate discovery cache after applying CRDs so the client can discover new resource types
	m.k8sClient.InvalidateDiscoveryCache()

	log.Info("Applying OVN-Kubernetes setup manifests...")
	for _, manifest := range setupManifests {
		manifestPath := filepath.Join(yamlDir, manifest)
		content, err := os.ReadFile(manifestPath)
		if err != nil {
			return fmt.Errorf("failed to read manifest %s: %w", manifest, err)
		}
		if err := m.k8sClient.ApplyManifest(content); err != nil {
			return fmt.Errorf("failed to apply manifest %s: %w", manifest, err)
		}
		log.Info("✓ Applied setup manifest %s", manifest)
	}

	// Label master nodes for OVN HA
	if err := m.labelOVNMasterNodes(); err != nil {
		return fmt.Errorf("failed to label master nodes: %w", err)
	}

	log.Info("Applying OVN-Kubernetes deployment manifests...")
	for _, manifest := range deploymentManifests {
		manifestPath := filepath.Join(yamlDir, manifest)
		content, err := os.ReadFile(manifestPath)
		if err != nil {
			return fmt.Errorf("failed to read manifest %s: %w", manifest, err)
		}
		if err := m.k8sClient.ApplyManifest(content); err != nil {
			return fmt.Errorf("failed to apply manifest %s: %w", manifest, err)
		}
		log.Info("✓ Applied deployment manifest %s", manifest)
	}

	return nil
}

// labelOVNMasterNodes labels master nodes for OVN HA deployment
func (m *CNIManager) labelOVNMasterNodes() error {
	log.Debug("Labeling master nodes for OVN-Kubernetes HA...")

	nodes, err := m.k8sClient.GetNodes()
	if err != nil {
		return fmt.Errorf("failed to get nodes: %w", err)
	}

	for _, node := range nodes {
		labels := map[string]string{
			"k8s.ovn.org/ovnkube-db":                "true",
			"node-role.kubernetes.io/control-plane": "",
		}
		if err := m.k8sClient.LabelNode(node.Name, labels); err != nil {
			return fmt.Errorf("failed to label node %s: %w", node.Name, err)
		}
		log.Debug("✓ Labeled node %s", node.Name)

		m.k8sClient.RemoveNodeTaint(node.Name, "node-role.kubernetes.io/master", corev1.TaintEffectNoSchedule)
		m.k8sClient.RemoveNodeTaint(node.Name, "node-role.kubernetes.io/control-plane", corev1.TaintEffectNoSchedule)
		log.Debug("✓ Removed taints from node %s", node.Name)
	}

	log.Info("✓ Master nodes labeled for OVN-Kubernetes HA")
	return nil
}

// installOVNKubernetes installs OVN-Kubernetes CNI using the local source code
func (m *CNIManager) installOVNKubernetes(clusterName string, k8sIP string, ovsNode bool) error {
	clusterCfg := m.config.GetClusterConfig(clusterName)
	if clusterCfg == nil {
		return fmt.Errorf("cluster configuration not found for cluster %s", clusterName)
	}
	podCIDR := clusterCfg.PodCIDR
	serviceCIDR := clusterCfg.ServiceCIDR
	apiServerURL := "https://" + k8sIP + ":6443"

	log.Info("For OVN-Kubernetes installation, using Pod CIDR: %s, Service CIDR: %s, API Server URL: %s", podCIDR, serviceCIDR, apiServerURL)

	if err := m.patchCoreDNS("8.8.8.8"); err != nil {
		return fmt.Errorf("failed to patch CoreDNS: %w", err)
	}

	ovnKPath, err := platform.EnsureOVNKubernetesSource()
	if err != nil {
		return fmt.Errorf("failed to ensure OVN-Kubernetes source: %w", err)
	}

	if err := m.runDaemonsetScript(ovnKPath, apiServerURL, podCIDR, serviceCIDR, DefaultOVNImage); err != nil {
		return fmt.Errorf("failed to run daemonset.sh: %w", err)
	}

	if err := m.applyOVNKubernetesManifests(ovnKPath, ovsNode); err != nil {
		return fmt.Errorf("failed to apply OVN-Kubernetes manifests: %w", err)
	}

	if err := m.k8sClient.WaitForPodsReady("ovn-kubernetes", "", 5*time.Minute); err != nil {
		log.Warn("Warning: OVN-Kubernetes pods may not be ready: %v", err)
	} else {
		log.Info("✓ OVN-Kubernetes pods are ready, installed successfully!")
	}

	if err := m.k8sClient.DeleteDaemonSet("kube-system", "kube-proxy"); err != nil {
		return fmt.Errorf("failed to delete kube-proxy DaemonSet: %w", err)
	}

	return nil
}
