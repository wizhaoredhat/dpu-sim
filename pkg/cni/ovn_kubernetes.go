package cni

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/containerengine"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

const (
	// DefaultOVNImage is the default OVN-Kubernetes image
	DefaultOVNImage = "ghcr.io/ovn-kubernetes/ovn-kubernetes/ovn-kube-fedora:master"

	// DefaultOVNRepoURL is the default URL for the OVN-Kubernetes repository
	DefaultOVNRepoURL = "https://github.com/ovn-org/ovn-kubernetes.git"
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
func (m *CNIManager) applyOVNKubernetesManifests(ovnPath string, ovsNode bool, clusterName string) error {
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
	if err := m.labelOVNMasterNodes(clusterName); err != nil {
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

// labelOVNMasterNodes labels only the master nodes (from config) for OVN HA deployment
func (m *CNIManager) labelOVNMasterNodes(clusterName string) error {
	log.Debug("Labeling master nodes for OVN-Kubernetes HA...")

	masterVMs := m.config.GetClusterRoleMapping()[clusterName][config.ClusterRoleMaster]
	for _, vm := range masterVMs {
		labels := map[string]string{
			"k8s.ovn.org/ovnkube-db":                "true",
			"node-role.kubernetes.io/control-plane": "",
		}
		if err := m.k8sClient.LabelNode(vm.Name, labels); err != nil {
			return fmt.Errorf("failed to label node %s: %w", vm.Name, err)
		}
		log.Debug("✓ Labeled node %s", vm.Name)

		m.k8sClient.RemoveNodeTaint(vm.Name, "node-role.kubernetes.io/master", corev1.TaintEffectNoSchedule)
		m.k8sClient.RemoveNodeTaint(vm.Name, "node-role.kubernetes.io/control-plane", corev1.TaintEffectNoSchedule)
		log.Debug("✓ Removed taints from node %s", vm.Name)
	}

	log.Info("✓ Master nodes labeled for OVN-Kubernetes HA")
	return nil
}

// redeployOVNKubernetes re-applies manifests and then deletes pods by label to
// force recreation
func (m *CNIManager) redeployOVNKubernetes(clusterName string) error {
	log.Info("Redeploying OVN-Kubernetes on cluster %s...", clusterName)

	localExec := platform.NewLocalExecutor()
	ovnKPath, err := EnsureOVNKubernetesSource(localExec)
	if err != nil {
		return fmt.Errorf("failed to ensure OVN-Kubernetes source: %w", err)
	}

	if err := m.applyOVNKubernetesManifests(ovnKPath, false, clusterName); err != nil {
		return fmt.Errorf("failed to re-apply OVN-Kubernetes manifests: %w", err)
	}

	ovnPodLabels := []string{"ovnkube-db", "ovnkube-master", "ovnkube-node", "ovnkube-identity"}
	for _, name := range ovnPodLabels {
		if err := m.k8sClient.DeletePodsByLabel("ovn-kubernetes", "name="+name); err != nil {
			log.Warn("Warning: failed to delete pods with label name=%s: %v", name, err)
		}
	}

	if err := m.k8sClient.WaitForPodsReady("ovn-kubernetes", "", 5*time.Minute); err != nil {
		log.Warn("Warning: OVN-Kubernetes pods may not be ready after redeploy: %v", err)
	} else {
		log.Info("✓ OVN-Kubernetes pods are ready after redeploy on cluster %s", clusterName)
	}

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

	localExec := platform.NewLocalExecutor()

	ovnKPath, err := EnsureOVNKubernetesSource(localExec)
	if err != nil {
		return fmt.Errorf("failed to ensure OVN-Kubernetes source: %w", err)
	}

	// Always build the OVN-Kubernetes image from source.
	if err := BuildOVNKubernetesImage(localExec, DefaultOVNKubeImage, ""); err != nil {
		return fmt.Errorf("failed to build OVN-Kubernetes image: %w", err)
	}

	// If a local registry is configured for OVN-Kubernetes, use the registry
	// image reference in daemonset manifests (the image was already built and
	// pushed by registry.Manager.SetupAll). Otherwise fall back to the
	// default upstream image.
	ovnImage := DefaultOVNImage
	regContainer := m.config.GetRegistryContainerForCNI(config.CNIOVNKubernetes)
	if regContainer != nil {
		ovnImage = m.config.GetRegistryImageRef(regContainer.Tag)
		log.Info("Using local registry image for OVN-Kubernetes daemonsets: %s", ovnImage)
	}

	if err := m.runDaemonsetScript(ovnKPath, apiServerURL, podCIDR, serviceCIDR, ovnImage); err != nil {
		return fmt.Errorf("failed to run daemonset.sh: %w", err)
	}

	if err := m.applyOVNKubernetesManifests(ovnKPath, ovsNode, clusterName); err != nil {
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

// getProjectRoot returns the root directory of the dpu-sim project
func getProjectRoot() (string, error) {
	// Get the directory of the current source file
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("failed to get current file path")
	}

	// Navigate from pkg/cni/ovn_kubernetes.go to project root (2 levels up from pkg/)
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(filename)))
	return projectRoot, nil
}

// getOVNKubernetesPath returns the path to the ovn-kubernetes directory
func getOVNKubernetesPath() (string, error) {
	projectRoot, err := getProjectRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(projectRoot, "ovn-kubernetes"), nil
}

// isOVNKubernetesPopulated checks if the ovn-kubernetes directory contains actual content
// An uninitialized submodule directory exists but is empty
func isOVNKubernetesPopulated(cmdExec platform.CommandExecutor, ovnPath string) bool {
	// Daemonset.sh is a dependency, check for its existence
	daemonsetScript := filepath.Join(ovnPath, "dist", "images", "daemonset.sh")
	exists, err := cmdExec.FileExists(daemonsetScript)
	if err != nil {
		log.Error("Failed to check if daemonset.sh exists: %v", err)
		return false
	}
	return exists
}

// initOVNKubernetesSubmodule initializes and updates the ovn-kubernetes git submodule
func initOVNKubernetesSubmodule(cmdExec platform.CommandExecutor, projectRoot string) error {
	log.Debug("Initializing ovn-kubernetes git submodule...")

	if err := cmdExec.RunCmdInDir(log.LevelInfo, projectRoot, "git", "submodule", "init", "ovn-kubernetes"); err != nil {
		return fmt.Errorf("failed to initialize submodule: %w", err)
	}

	if err := cmdExec.RunCmdInDir(log.LevelInfo, projectRoot, "git", "submodule", "update", "--init", "ovn-kubernetes"); err != nil {
		return fmt.Errorf("failed to update submodule: %w", err)
	}

	log.Info("✓ ovn-kubernetes submodule is initialized")
	return nil
}

// EnsureOVNKubernetesSource ensures the ovn-kubernetes source code is available.
// It first tries to initialize the git submodule if it exists but is empty.
// If submodule initialization fails or the directory doesn't exist, it clones the repository.
func EnsureOVNKubernetesSource(cmdExec platform.CommandExecutor) (string, error) {
	ovnPath, err := getOVNKubernetesPath()
	if err != nil {
		return "", fmt.Errorf("failed to get OVN-Kubernetes path: %w", err)
	}

	projectRoot, err := getProjectRoot()
	if err != nil {
		return "", fmt.Errorf("failed to get project root: %w", err)
	}

	// Check if directory exists and is populated
	exists, err := cmdExec.FileExists(ovnPath)
	if err != nil {
		return "", fmt.Errorf("failed to check OVN-Kubernetes path: %w", err)
	}

	if exists {
		if isOVNKubernetesPopulated(cmdExec, ovnPath) {
			log.Debug("OVN-Kubernetes source found at %s", ovnPath)
			return ovnPath, nil
		}

		// Directory exists but is empty (uninitialized submodule)
		log.Info("OVN-Kubernetes directory exists but appears empty (uninitialized submodule)")
		if err := initOVNKubernetesSubmodule(cmdExec, projectRoot); err != nil {
			log.Warn("Warning: Failed to initialize submodule: %v", err)
			log.Info("Attempting to clone repository directly...")

			// Remove the empty directory and clone fresh
			if err := cmdExec.RemoveAll(ovnPath); err != nil {
				return "", fmt.Errorf("failed to remove empty ovn-kubernetes directory: %w", err)
			}
		} else {
			// Submodule initialized successfully
			if isOVNKubernetesPopulated(cmdExec, ovnPath) {
				return ovnPath, nil
			}
			return "", fmt.Errorf("submodule initialized but content still missing")
		}
	}

	// Directory doesn't exist or was removed - try submodule init first, then clone as fallback
	gitDir := filepath.Join(projectRoot, ".git")
	gitDirExists, _ := cmdExec.FileExists(gitDir)
	if gitDirExists {
		// We're in a git repository, try submodule init
		if err := initOVNKubernetesSubmodule(cmdExec, projectRoot); err == nil {
			if isOVNKubernetesPopulated(cmdExec, ovnPath) {
				return ovnPath, nil
			}
		}
		log.Info("Submodule initialization failed, falling back to clone...")
	}

	log.Info("OVN-Kubernetes not found, cloning from %s:master...", DefaultOVNRepoURL)
	if err := cmdExec.RunCmdInDir(log.LevelInfo, projectRoot, "git", "clone", "--branch", "master", DefaultOVNRepoURL, ovnPath); err != nil {
		return "", fmt.Errorf("failed to clone OVN-Kubernetes repository: %w", err)
	}

	log.Info("✓ OVN-Kubernetes is cloned to %s", ovnPath)
	return ovnPath, nil
}

// BuildOVNKubernetesImage builds the OVN-Kubernetes container image from the local
// source code using the Dockerfile.fedora in ovn-kubernetes/dist/images/.
// imageName specifies the tag for the built image (e.g., "ovn-kube-fedora:latest").
// By default, OVN/OVS RPMs are downloaded from Koji. To build OVN from source instead,
// set ovnGitRef to a branch/tag/commit (e.g., "main"); pass an empty string for Koji.
func BuildOVNKubernetesImage(cmdExec platform.CommandExecutor, imageName string, ovnGitRef string) error {
	engine, err := containerengine.NewProjectEngine(cmdExec)
	if err != nil {
		return err
	}
	return BuildOVNKubernetesImageWithEngine(cmdExec, engine, imageName, ovnGitRef)
}

// BuildOVNKubernetesImageWithEngine builds the image using a preselected
// container engine so callers can detect once and reuse it.
func BuildOVNKubernetesImageWithEngine(
	cmdExec platform.CommandExecutor,
	engine containerengine.Engine,
	imageName string,
	ovnGitRef string,
) error {
	ctx := context.Background()
	ovnPath, err := EnsureOVNKubernetesSource(cmdExec)
	if err != nil {
		return fmt.Errorf("failed to ensure OVN-Kubernetes source: %w", err)
	}

	dockerfile := filepath.Join(ovnPath, "dist", "images", "Dockerfile.fedora")
	exists, err := cmdExec.FileExists(dockerfile)
	if err != nil {
		return fmt.Errorf("failed to check for Dockerfile.fedora: %w", err)
	}
	if !exists {
		return fmt.Errorf("Dockerfile.fedora not found at %s", dockerfile)
	}
	dockerfileContent, err := cmdExec.ReadFile(dockerfile)
	if err != nil {
		return fmt.Errorf("failed to read Dockerfile.fedora: %w", err)
	}

	// Write a .dockerignore to the build context to reduce its size and improve
	// layer cache stability. The COPY in the Dockerfile sends all files from the
	// build context; without this, docs/, test/, .github/, helm/, etc. are
	// included unnecessarily. Any change in those directories would also
	// invalidate the COPY layer cache and trigger a full Go rebuild.
	if err := writeDockerignore(cmdExec, ovnPath); err != nil {
		log.Warn("Warning: failed to write .dockerignore, build may be slower: %v", err)
	}
	defer cleanupDockerignore(cmdExec, ovnPath)

	// Detect architecture from the executor's target system.
	targetArch, err := cmdExec.GetArchitecture()
	if err != nil {
		return fmt.Errorf("failed to detect architecture: %w", err)
	}
	arch := targetArch.GoArch()
	isPodman := engine.Name() == containerengine.EnginePodman

	// Build the Go builder image reference.
	const goVersion = "1.24"
	goImage := fmt.Sprintf("quay.io/projectquay/golang:%s", goVersion)

	// Determine OVN_FROM: "koji" (pre-built RPMs) or "source" (build from git).
	ovnFrom := "koji"
	// When building OVN from source, resolve the git ref to a SHA and pass it.
	if ovnGitRef != "" {
		ovnFrom = "source"
	}

	resolvedOVNGitRef := ""
	buildOpts := containerengine.BuildOptions{
		ContextDir: ovnPath,
		Dockerfile: dockerfile,
		Platform:   "linux/" + arch,
		BuildArgs: map[string]string{
			"BUILDER_IMAGE":      goImage,
			"OVN_FROM":           ovnFrom,
			"OVN_KUBERNETES_DIR": ".",
			// Podman does not auto-populate BUILDPLATFORM/TARGETOS/TARGETARCH
			// the way Docker BuildKit does. Pass them explicitly so the
			// Dockerfile's cross-compilation logic works correctly.
			"BUILDPLATFORM": "linux/" + arch,
			"TARGETOS":      "linux",
			"TARGETARCH":    arch,
		},
	}

	// When using podman, mount a persistent host directory for the Go build cache.
	// podman build --volume requires absolute host paths (no named volumes).
	// Without this, every COPY layer change wipes the Go compiler cache and
	// forces a full recompilation of all ~1000 packages (~5+ min).
	// With the cache, only changed packages are recompiled.
	// The culprit is the _output/ directory inside go-controller/ which linger in the
	// tree and have different timestamps & content each time. Hence the need for a
	// persistent Go build cache inside the build container.
	if isPodman {
		cacheDir, err := getGoBuildCacheDir()
		if err != nil {
			log.Warn("Warning: could not create Go build cache dir, build may be slower: %v", err)
		} else {
			buildOpts.ExtraArgs = append(buildOpts.ExtraArgs,
				"--volume", cacheDir+":/root/.cache/go-build:Z",
				"--volume", cacheDir+":/go/pkg/mod:Z",
			)
		}
	}

	if ovnGitRef != "" {
		ovnRepo := "https://github.com/ovn-org/ovn.git"
		sha, err := resolveGitRef(cmdExec, ovnRepo, ovnGitRef)
		if err != nil {
			return fmt.Errorf("failed to resolve OVN git ref %q: %w", ovnGitRef, err)
		}
		resolvedOVNGitRef = sha
		buildOpts.BuildArgs["OVN_REPO"] = ovnRepo
		buildOpts.BuildArgs["OVN_GITREF"] = sha
	}

	// Build a deterministic cache key from source/config inputs so any relevant
	// OVN-Kubernetes or build configuration change triggers a new image tag.
	sourceRev, err := ovnKubernetesSourceRevision(cmdExec, ovnPath)
	if err != nil {
		log.Warn("Warning: could not determine OVN-Kubernetes source revision, falling back to non-cached tag: %v", err)
		sourceRev = "unknown"
	}
	cacheKey := ovnKubeImageCacheKey(sourceRev, arch, ovnFrom, resolvedOVNGitRef, dockerfileContent)
	cachedImageName := ovnKubeCachedImageName(imageName, cacheKey)

	// Fast path: reuse existing cached image and retag to requested image name for
	// backward compatibility with downstream registry/manifests flow.
	if engine.ImageExists(ctx, cachedImageName) {
		log.Info("Using cached OVN-Kubernetes image %s", cachedImageName)
		if cachedImageName != imageName {
			if err := engine.Tag(ctx, cachedImageName, imageName); err != nil {
				return fmt.Errorf("failed to tag cached image %s as %s: %w", cachedImageName, imageName, err)
			}
		}
		return nil
	}

	buildOpts.Image = cachedImageName
	log.Info("Building OVN-Kubernetes image %s (cache=%s, OVN_FROM=%s, arch=%s)...", imageName, cacheKey, ovnFrom, arch)
	if err := engine.Build(ctx, buildOpts); err != nil {
		return fmt.Errorf("failed to build OVN-Kubernetes image: %w", err)
	}

	if cachedImageName != imageName {
		if err := engine.Tag(ctx, cachedImageName, imageName); err != nil {
			return fmt.Errorf("failed to tag built image %s as %s: %w", cachedImageName, imageName, err)
		}
	}

	log.Info("✓ OVN-Kubernetes image built successfully: %s (cached as %s)", imageName, cachedImageName)
	return nil
}

// ovnKubernetesSourceRevision returns a source fingerprint based on HEAD, and
// when dirty, appends a hash of tracked-file diffs so local changes invalidate cache.
func ovnKubernetesSourceRevision(cmdExec platform.CommandExecutor, ovnPath string) (string, error) {
	stdout, _, err := cmdExec.Execute(fmt.Sprintf("git -C %q rev-parse HEAD", ovnPath))
	if err != nil {
		return "", err
	}
	head := strings.TrimSpace(stdout)
	if head == "" {
		return "", fmt.Errorf("empty git HEAD")
	}

	stdout, _, err = cmdExec.Execute(fmt.Sprintf("git -C %q status --porcelain --untracked-files=no", ovnPath))
	if err != nil {
		return head, nil
	}
	if strings.TrimSpace(stdout) == "" {
		return head, nil
	}

	diffOut, _, err := cmdExec.Execute(fmt.Sprintf("git -C %q diff --no-ext-diff --binary -- .", ovnPath))
	if err != nil {
		return head + "-dirty", nil
	}
	return head + "-dirty-" + shortSHA256(diffOut, 12), nil
}

// ovnKubeImageCacheKey generates a stable image-cache key from build inputs.
func ovnKubeImageCacheKey(sourceRev, arch, ovnFrom, ovnGitRef string, dockerfileContent []byte) string {
	material := strings.Join([]string{
		sourceRev,
		"arch=" + arch,
		"ovn_from=" + ovnFrom,
		"ovn_gitref=" + ovnGitRef,
		"dockerfile_sha=" + shortSHA256Bytes(dockerfileContent, 16),
	}, "\n")
	return shortSHA256(material, 16)
}

// ovnKubeCachedImageName appends cacheKey to the existing tag component.
func ovnKubeCachedImageName(imageName, cacheKey string) string {
	lastColon := strings.LastIndex(imageName, ":")
	lastSlash := strings.LastIndex(imageName, "/")
	if lastColon > lastSlash {
		repo := imageName[:lastColon]
		tag := imageName[lastColon+1:]
		return fmt.Sprintf("%s:%s-%s", repo, tag, cacheKey)
	}
	return fmt.Sprintf("%s:%s", imageName, cacheKey)
}

func shortSHA256(s string, n int) string {
	sum := sha256.Sum256([]byte(s))
	encoded := hex.EncodeToString(sum[:])
	if n <= 0 || n >= len(encoded) {
		return encoded
	}
	return encoded[:n]
}

func shortSHA256Bytes(b []byte, n int) string {
	sum := sha256.Sum256(b)
	encoded := hex.EncodeToString(sum[:])
	if n <= 0 || n >= len(encoded) {
		return encoded
	}
	return encoded[:n]
}

// getGoBuildCacheDir returns an absolute path to a persistent directory used
// to cache Go build artifacts across podman builds. The directory is created
// under the user's cache directory if it does not already exist.
func getGoBuildCacheDir() (string, error) {
	cacheHome, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user cache dir: %w", err)
	}
	dir := filepath.Join(cacheHome, "dpu-sim", "ovn-go-build-cache")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache dir %s: %w", dir, err)
	}
	return dir, nil
}

// writeDockerignore writes a .dockerignore file to the build context directory.
// This excludes directories that are not needed for the container build, which
// reduces the build context size and prevents unrelated file changes from
// invalidating Docker's layer cache for the COPY instruction.
func writeDockerignore(cmdExec platform.CommandExecutor, buildContextDir string) error {
	// Only exclude directories that are NOT needed by Dockerfile.fedora:
	//   - The build needs: go-controller/ (source + vendor), dist/ (Makefile, scripts)
	//   - Everything else is documentation, tests, CI config, etc.
	content := strings.Join([]string{
		"# Auto-generated by dpu-sim to speed up docker builds.",
		"# Excludes files not needed by Dockerfile.fedora so the COPY layer",
		"# cache is only invalidated when actual build inputs change.",
		".git",
		".github",
		"contrib",
		"docs",
		"test",
		"helm",
		"contrib",
		"*.yml",
		"*.txt",
		"*.md",
		"**/*_test.go",
		"",
	}, "\n")

	dockerignorePath := filepath.Join(buildContextDir, ".dockerignore")
	return cmdExec.WriteFile(dockerignorePath, []byte(content), 0644)
}

// cleanupDockerignore removes the .dockerignore written by writeDockerignore.
// This keeps the submodule directory clean after the build.
func cleanupDockerignore(cmdExec platform.CommandExecutor, buildContextDir string) {
	dockerignorePath := filepath.Join(buildContextDir, ".dockerignore")
	if err := cmdExec.RemoveAll(dockerignorePath); err != nil {
		log.Debug("Note: failed to remove .dockerignore at %s: %v", dockerignorePath, err)
	}
}

// resolveGitRef resolves a git ref (branch, tag, or commit) to a full SHA using ls-remote.
func resolveGitRef(cmdExec platform.CommandExecutor, repo, ref string) (string, error) {
	stdout, _, err := cmdExec.Execute(fmt.Sprintf("git ls-remote '%s' '%s'", repo, ref))
	if err != nil {
		return "", fmt.Errorf("git ls-remote failed: %w", err)
	}

	lines := strings.TrimSpace(stdout)
	if lines == "" {
		// The ref might already be a commit SHA; return it as-is
		return ref, nil
	}

	// Take the first line and extract the SHA
	parts := strings.Fields(lines)
	if len(parts) < 1 {
		return ref, nil
	}
	return parts[0], nil
}
