package requirements

import (
	"fmt"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/linux"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

// EnsureDependencies checks and installs all dpu-sim dependencies on the local machine
// cfg provides configuration including version information for dependencies
// Returns an error if any dependency cannot be installed
func EnsureDependencies(cfg *config.Config) error {
	// Create a local executor
	exec := platform.NewLocalExecutor()
	deps, err := getRequiredDependencies(cfg)
	if err != nil {
		return fmt.Errorf("failed to get dependencies: %w", err)
	}
	return platform.EnsureDependenciesWithExecutor(exec, deps, cfg)
}

// getRequiredDependencies returns the list of dependencies needed by dpu-sim
func getRequiredDependencies(cfg *config.Config) ([]platform.Dependency, error) {
	// Common dependencies needed by the tool
	deps := []platform.Dependency{
		{
			Name:        "wget",
			Reason:      "Required for downloading images",
			CheckCmd:    []string{"wget", "--version"},
			InstallFunc: linux.InstallGenericPackage,
		},
		{
			Name:        "pip3",
			Reason:      "Required for OVN-Kubernetes daemonset.sh script",
			CheckCmd:    []string{"pip3", "--version"},
			InstallFunc: linux.InstallGenericPackage,
		},
		{
			Name:        "jinjanator",
			Reason:      "Required for OVN-Kubernetes daemonset.sh script",
			CheckCmd:    []string{"jinjanate", "--version"},
			InstallFunc: linux.InstallJinjanator,
		},
		{
			Name:        "git",
			Reason:      "Required for OVN-Kubernetes git submodule",
			CheckCmd:    []string{"git", "--version"},
			InstallFunc: linux.InstallGenericPackage,
		},
		{
			Name:        "openvswitch",
			Reason:      "Required for OVS bridged networks",
			CheckCmd:    []string{"ovs-vsctl", "--version"},
			InstallFunc: linux.InstallOpenVSwitch,
		},
	}

	mode, err := cfg.GetDeploymentMode()
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment mode: %w", err)
	}

	switch mode {
	case config.VMDeploymentMode:
		deps = append(deps, platform.Dependency{
			Name:        "libvirt",
			Reason:      "Required for VM management",
			CheckCmd:    []string{"virsh", "--version"},
			InstallFunc: linux.InstallGenericPackage,
		})
		deps = append(deps, platform.Dependency{
			Name:        "qemu-kvm",
			Reason:      "Required for VM management",
			CheckFunc:   linux.CheckGenericPackage,
			InstallFunc: linux.InstallGenericPackage,
		})
		deps = append(deps, platform.Dependency{
			Name:        "qemu-img",
			Reason:      "Required for VM management",
			CheckCmd:    []string{"qemu-img", "--version"},
			InstallFunc: linux.InstallGenericPackage,
		})
		deps = append(deps, platform.Dependency{
			Name:        "libvirt-devel",
			Reason:      "Required for VM management",
			CheckFunc:   linux.CheckGenericPackage,
			InstallFunc: linux.InstallGenericPackage,
		})
		deps = append(deps, platform.Dependency{
			Name:        "virt-install",
			Reason:      "Required for VM management",
			CheckCmd:    []string{"virt-install", "--version"},
			InstallFunc: linux.InstallGenericPackage,
		})
		deps = append(deps, platform.Dependency{
			Name:        "genisoimage",
			Reason:      "Required for VM cloud-init ISOs",
			CheckCmd:    []string{"genisoimage", "-version"},
			InstallFunc: linux.InstallGenericPackage,
		})
	case config.KindDeploymentMode:
		deps = append(deps, platform.Dependency{
			Name:        "kubectl",
			Reason:      "Required for cluster management",
			CheckCmd:    []string{"kubectl"},
			InstallFunc: linux.InstallKubectl,
		})
		deps = append(deps, platform.Dependency{
			Name:        "Container Runtime",
			Reason:      "Required for Kind",
			CheckCmd:    []string{"podman", "ps"},
			InstallFunc: linux.InstallContainerRuntime,
		})
		deps = append(deps, platform.Dependency{
			Name:        "kind",
			Reason:      "Required for Kind clusters",
			CheckCmd:    []string{"kind", "version"},
			InstallFunc: linux.InstallKind,
		})
	}
	return deps, nil
}
