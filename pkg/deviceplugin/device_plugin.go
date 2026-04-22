package deviceplugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wizhao/dpu-sim/pkg/containerengine"
	"github.com/wizhao/dpu-sim/pkg/k8s"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
	"github.com/wizhao/dpu-sim/pkg/registry"
)

const (
	DevicePluginImage         = "dpu-sim-dp:latest"
	DefaultDevicePluginImage  = "quay.io/wizhao/dpu-sim-dp:latest"
	DevicePluginDaemonSetName = "dpu-sim-device-plugin"
	DevicePluginNamespace     = "kube-system"

	MgmtVFResourceName = "dpusim.io/mgmtvf"
	// VFResourceName is the extended-resource name for simulated VFs.
	VFResourceName = "dpusim.io/vf"
)

// ResourcePool describes one class of simulated device resources.
// Each pool gets its own gRPC socket, kubelet registration, and env var.
type ResourcePool struct {
	// ResourceName is the extended-resource name advertised to kubelet
	// (e.g. "dpusim.io/mgmtvf").
	ResourceName string

	// SocketName is the filename of the Unix socket under
	// /var/lib/kubelet/device-plugins/ (must be unique per pool).
	SocketName string

	// EnvVarName is the environment variable injected into containers on
	// allocation (mirrors the PCIDEVICE_* convention from SR-IOV Device Plugin).
	EnvVarName string

	// IfaceRegex selects which host interfaces belong to this pool.
	IfaceRegex *regexp.Regexp
}

var ResourcePools = []ResourcePool{
	/* TODO: Enable when we set Management port through resource pool.
	{
		ResourceName: MgmtVFResourceName,
		SocketName:   "dpusim-mgmtvf.sock",
		EnvVarName:   "PCIDEVICE_DPUSIM_IO_MGMTVF",
		IfaceRegex:   regexp.MustCompile(`^eth0-1$`),
	},
	*/
	{
		ResourceName: VFResourceName,
		SocketName:   "dpusim-vf.sock",
		EnvVarName:   "PCIDEVICE_DPUSIM_IO_VF",
		// matches eth0-2, eth0-3, …, eth0-10, etc. (any eth0-* except eth0-0 and eth0-1).
		// The regex ^eth0-(?:[2-9]|\d{2,})$ matches single digits 2–9 or any number with
		// 2+ digits (i.e., 10 and above).
		IfaceRegex: regexp.MustCompile(`^eth0-(?:[2-9]|\d{2,})$`),
	},
}

// BuildAndLoadImage builds the device plugin image and pushes it to the
// provided image loader (e.g. a local registry). Returns the image reference
// that Kubernetes manifests should use.
func BuildAndLoadImage(cmdExec platform.CommandExecutor, engine containerengine.Engine, loader registry.ImageLoader) (string, error) {
	if err := BuildDevicePluginImage(cmdExec, engine); err != nil {
		return "", err
	}
	return loader.LoadImage(DevicePluginImage, DevicePluginImage)
}

// BuildDevicePluginImage builds the dpu-sim device plugin container image
// from the device plugin Dockerfile.
func BuildDevicePluginImage(cmdExec platform.CommandExecutor, engine containerengine.Engine) error {
	projectRoot, err := platform.GetProjectRoot()
	if err != nil {
		return fmt.Errorf("failed to get project root: %w", err)
	}

	dockerfile := filepath.Join(projectRoot, "deploy", "device-plugin", "Dockerfile")
	exists, err := cmdExec.FileExists(dockerfile)
	if err != nil {
		return fmt.Errorf("failed to check Dockerfile: %w", err)
	}
	if !exists {
		return fmt.Errorf("device plugin Dockerfile not found at %s", dockerfile)
	}

	targetArch, err := cmdExec.GetArchitecture()
	if err != nil {
		return fmt.Errorf("failed to detect architecture: %w", err)
	}

	buildOpts := containerengine.BuildOptions{
		ContextDir: projectRoot,
		Dockerfile: dockerfile,
		Image:      DevicePluginImage,
		Platform:   "linux/" + targetArch.GoArch(),
	}

	log.Info("Building Device Plugin image %s (Architecture=%s)...", DevicePluginImage, targetArch)
	if err := engine.Build(context.Background(), buildOpts); err != nil {
		return fmt.Errorf("failed to build Device Plugin image: %w", err)
	}

	log.Info("✓ Device Plugin image built: %s", DevicePluginImage)
	return nil
}

// deployDevicePlugin deploys the simulated device plugin DaemonSet onto the
// current cluster. The manifest template is read from deploy/device-plugin/
// and the image placeholder is replaced with the actual image reference.
func DeployDevicePlugin(k8sClient *k8s.K8sClient, imageRef string) error {
	projectRoot, err := platform.GetProjectRoot()
	if err != nil {
		return fmt.Errorf("failed to get project root: %w", err)
	}

	manifestPath := filepath.Join(projectRoot, "deploy", "device-plugin", "daemonset.yaml")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read device plugin DaemonSet manifest: %w", err)
	}

	manifest := strings.ReplaceAll(string(manifestBytes), "DPU_SIM_DP_IMAGE", imageRef)

	log.Info("Deploying Device Plugin DaemonSet (image=%s)...", imageRef)
	if err := k8sClient.ApplyManifest([]byte(manifest)); err != nil {
		return fmt.Errorf("failed to apply Device Plugin DaemonSet: %w", err)
	}

	if err := k8sClient.WaitForPodsReady(DevicePluginNamespace, "app="+DevicePluginDaemonSetName, 3*time.Minute); err != nil {
		log.Warn("Warning: Device Plugin DaemonSet pods may not be ready: %v", err)
	} else {
		log.Info("✓ Device Plugin DaemonSet is ready")
	}

	return nil
}
