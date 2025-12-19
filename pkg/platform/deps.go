// Package platform provides utilities for Linux distribution detection and management
package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wizhao/dpu-sim/pkg/config"
)

const (
	// DefaultOVNRepoURL is the default URL for the OVN-Kubernetes repository
	DefaultOVNRepoURL = "https://github.com/ovn-org/ovn-kubernetes.git"
)

// GetProjectRoot returns the root directory of the dpu-sim project
func GetProjectRoot() (string, error) {
	// Get the directory of the current source file
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("failed to get current file path")
	}

	// Navigate from pkg/linux/deps.go to project root (2 levels up)
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(filename)))
	return projectRoot, nil
}

// InstallFunc is a function that installs a package dependency
// It receives the detected distro and config for platform-specific installation
type InstallFunc func(distro *Distro, cfg *config.Config) error

// Dependency represents a tool dependency
type Dependency struct {
	Name        string      // Name of the dependency
	Reason      string      // Reason for the dependency
	CheckCmd    []string    // Command to check if dependency is installed
	InstallFunc InstallFunc // Function to install the dependency
}

// DependencyResult holds the result of checking a dependency
type DependencyResult struct {
	Name      string // Name of the dependency
	Installed bool   // True if the dependency is installed
	Output    string // Output of the check command
	Error     error  // Output error of the check command
}

// ensureLocalBinInPath adds ~/.local/bin to PATH if not already present
func ensureLocalBinInPath() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	localBin := filepath.Join(homeDir, ".local", "bin")
	currentPath := os.Getenv("PATH")
	if !strings.Contains(currentPath, localBin) {
		os.Setenv("PATH", localBin+":"+currentPath)
	}
}

// runCmd executes a command and returns any error
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func unsupportedPackageManager(distro *Distro) error {
	return fmt.Errorf("unsupported package manager: %s", distro.PackageManager)
}

func unsupportedArchitecture(distro *Distro) error {
	return fmt.Errorf("unsupported architecture: %s", distro.Architecture)
}

// installPip3 installs pip3 using the system package manager
func installPip3(distro *Distro, cfg *config.Config) error {
	switch distro.PackageManager {
	case DNF:
		if err := runCmd("sudo", DNF, "install", "-y", "python3-pip"); err != nil {
			return fmt.Errorf("failed to install python3-pip: %w", err)
		}
	default:
		return unsupportedPackageManager(distro)
	}
	return nil
}

// installJinjanator installs jinjanator via pip3
func installJinjanator(distro *Distro, cfg *config.Config) error {
	// First ensure ~/.local/bin is in PATH
	ensureLocalBinInPath()
	if err := runCmd("pip3", "install", "--user", "jinjanator[yaml]"); err != nil {
		return fmt.Errorf("failed to install jinjanator: %w", err)
	}
	return nil
}

// installGit installs git using the system package manager
func installGit(distro *Distro, cfg *config.Config) error {
	switch distro.PackageManager {
	case DNF:
		if err := runCmd("sudo", DNF, "install", "-y", "git"); err != nil {
			return fmt.Errorf("failed to install git: %w", err)
		}
	default:
		return unsupportedPackageManager(distro)
	}
	return nil
}

// installKubectl installs kubectl by adding the Kubernetes repository
func installKubectl(distro *Distro, cfg *config.Config) error {
	version := cfg.Kubernetes.Version
	switch distro.PackageManager {
	case DNF:
		// Add Kubernetes repository for Fedora/RHEL/CentOS
		repoContent := fmt.Sprintf(`[kubernetes]
name=Kubernetes
baseurl=https://pkgs.k8s.io/core:/stable:/%s/rpm/
enabled=1
gpgcheck=1
gpgkey=https://pkgs.k8s.io/core:/stable:/%s/rpm/repodata/repomd.xml.key
`, version, version)
		if err := os.WriteFile("/tmp/kubernetes.repo", []byte(repoContent), 0644); err != nil {
			return fmt.Errorf("failed to write repo file: %w", err)
		}
		if err := runCmd("sudo", "mv", "/tmp/kubernetes.repo", "/etc/yum.repos.d/kubernetes.repo"); err != nil {
			return fmt.Errorf("failed to install repo file: %w", err)
		}
		if err := runCmd("sudo", DNF, "install", "-y", "kubectl"); err != nil {
			return fmt.Errorf("failed to install kubectl: %w", err)
		}
	default:
		return unsupportedPackageManager(distro)
	}
	return nil
}

// installDocker installs Docker using the official installation method
func installDocker(distro *Distro, cfg *config.Config) error {
	switch distro.PackageManager {
	case DNF:
		if err := runCmd("sudo", DNF, "install", "-y", "podman"); err != nil {
			return fmt.Errorf("failed to install podman: %w", err)
		}
		if err := runCmd("sudo", DNF, "install", "-y", "docker"); err != nil {
			return fmt.Errorf("failed to install docker: %w", err)
		}
		if err := runCmd("sudo", "systemctl", "start", "podman"); err != nil {
			return fmt.Errorf("failed to start podman: %w", err)
		}
		if err := runCmd("sudo", "systemctl", "enable", "podman"); err != nil {
			return fmt.Errorf("failed to enable podman: %w", err)
		}
	default:
		return unsupportedPackageManager(distro)
	}
	return nil
}

// installKind installs Kind (Kubernetes in Docker)
func installKind(distro *Distro, cfg *config.Config) error {
	if distro.Architecture == X86_64 {
		if err := runCmd("curl", "-Lo", "./kind", "https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64"); err != nil {
			return fmt.Errorf("failed to download kind: %w", err)
		}
	} else {
		return unsupportedArchitecture(distro)
	}

	if err := runCmd("chmod", "+x", "./kind"); err != nil {
		return fmt.Errorf("failed to chmod kind: %w", err)
	}
	if err := runCmd("sudo", "mv", "./kind", "/usr/local/bin/kind"); err != nil {
		return fmt.Errorf("failed to move kind to /usr/local/bin: %w", err)
	}
	return nil
}

func installOVS(distro *Distro, cfg *config.Config) error {
	switch distro.PackageManager {
	case DNF:
		if distro.Architecture == X86_64 {
			if err := runCmd("sudo", "subscription-manager", "repos", "--enable=openstack-17-for-rhel-9-x86_64-rpms"); err != nil {
				return fmt.Errorf("failed to enable openstack-17-for-rhel-9-x86_64-rpms: %w", err)
			}
		} else {
			return unsupportedArchitecture(distro)
		}
		if err := runCmd("sudo", DNF, "install", "-y", "openvswitch"); err != nil {
			return fmt.Errorf("failed to install openvswitch: %w", err)
		}
	default:
		return unsupportedPackageManager(distro)
	}
	return nil
}

func installGenisoimage(distro *Distro, cfg *config.Config) error {
	switch distro.PackageManager {
	case DNF:
		if err := runCmd("sudo", DNF, "install", "-y", "genisoimage"); err != nil {
			return fmt.Errorf("failed to install genisoimage: %w", err)
		}
	default:
		return unsupportedPackageManager(distro)
	}
	return nil
}

// GetDependencies returns the list of dependencies needed by dpu-sim
func GetDependencies() []Dependency {
	return []Dependency{
		{
			Name:        "pip3",
			Reason:      "Required for OVN-Kubernetes daemonset.sh script",
			CheckCmd:    []string{"pip3", "--version"},
			InstallFunc: installPip3,
		},
		{
			Name:        "jinjanator",
			Reason:      "Required for OVN-Kubernetes daemonset.sh script",
			CheckCmd:    []string{"jinjanate", "--version"},
			InstallFunc: installJinjanator,
		},
		{
			Name:        "git",
			Reason:      "Required for OVN-Kubernetes git submodule",
			CheckCmd:    []string{"git", "--version"},
			InstallFunc: installGit,
		},
		{
			Name:        "kubectl",
			Reason:      "Required for cluster management",
			CheckCmd:    []string{"kubectl"},
			InstallFunc: installKubectl,
		},
		{
			Name:        "docker",
			Reason:      "Required for Kind",
			CheckCmd:    []string{"docker", "--version"},
			InstallFunc: installDocker,
		},
		{
			Name:        "kind",
			Reason:      "Required for Kind clusters",
			CheckCmd:    []string{"kind", "version"},
			InstallFunc: installKind,
		},
		{
			Name:        "ovs",
			Reason:      "Required for OVS Networks",
			CheckCmd:    []string{"ovs-vsctl", "--version"},
			InstallFunc: installOVS,
		},
		{
			Name:        "genisoimage",
			Reason:      "Required for VM cloud-init ISOs",
			CheckCmd:    []string{"genisoimage", "-version"},
			InstallFunc: installGenisoimage,
		},
	}
}

// checkDependency checks if a single dependency is installed
func checkDependency(dep Dependency) DependencyResult {
	result := DependencyResult{
		Name:      dep.Name,
		Installed: false,
	}

	if len(dep.CheckCmd) == 0 {
		result.Error = fmt.Errorf("no check command defined")
		return result
	}

	cmd := exec.Command(dep.CheckCmd[0], dep.CheckCmd[1:]...)
	output, err := cmd.CombinedOutput()
	result.Output = string(output)
	if err != nil {
		result.Error = err
		return result
	}

	result.Installed = true
	return result
}

// installDependency attempts to install a dependency using its install function
func installDependency(dep Dependency, distro *Distro, cfg *config.Config) error {
	if dep.InstallFunc == nil {
		return fmt.Errorf("no install function defined for %s", dep.Name)
	}

	fmt.Printf("Installing %s for %s...\n", dep.Name, distro.ID)

	if err := dep.InstallFunc(distro, cfg); err != nil {
		return fmt.Errorf("failed to install %s: %w. Needed for %s", dep.Name, err, dep.Reason)
	}

	fmt.Printf("✓ %s installed\n", dep.Name)
	return nil
}

// EnsureDependencies checks and installs all dpu-sim dependencies
// cfg provides configuration including version information for dependencies
// Returns an error if any dependency cannot be installed
func EnsureDependencies(cfg *config.Config) error {
	fmt.Println("Checking dependencies...")

	// Ensure ~/.local/bin is in PATH for pip user installs
	ensureLocalBinInPath()

	// Detect local distro for installation
	distro, err := DetectLocalDistro()
	if err != nil {
		return fmt.Errorf("failed to detect local distribution: %w", err)
	}
	fmt.Printf("Detected Linux distribution: %s %s (package manager: %s, architecture: %s)\n", distro.ID, distro.VersionID, distro.PackageManager, distro.Architecture)

	deps := GetDependencies()
	var missing []Dependency

	for _, dep := range deps {
		result := checkDependency(dep)
		if result.Installed {
			fmt.Printf("✓ %s is installed\n", dep.Name)
		} else {
			fmt.Printf("✗ %s is not installed\n", dep.Name)
			missing = append(missing, dep)
		}
	}

	if len(missing) > 0 {
		var names []string
		for _, dep := range missing {
			names = append(names, dep.Name)
		}
		fmt.Printf("Installing missing dependencies: %s\n", strings.Join(names, ", "))
		for _, dep := range missing {
			if err := installDependency(dep, distro, cfg); err != nil {
				return fmt.Errorf("failed to install dependency %s: %w", dep.Name, err)
			}

			result := checkDependency(dep)
			if !result.Installed {
				return fmt.Errorf("dependency %s was installed but verification failed", dep.Name)
			}
		}
	}

	fmt.Println("✓ All dependencies are available")
	return nil
}

// GetOVNKubernetesPath returns the path to the ovn-kubernetes directory
func GetOVNKubernetesPath() (string, error) {
	projectRoot, err := GetProjectRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(projectRoot, "ovn-kubernetes"), nil
}

// isOVNKubernetesPopulated checks if the ovn-kubernetes directory contains actual content
// An uninitialized submodule directory exists but is empty
func isOVNKubernetesPopulated(ovnPath string) bool {
	// Daemonset.sh is a dependency, check for its existence
	daemonsetScript := filepath.Join(ovnPath, "dist", "images", "daemonset.sh")
	if _, err := os.Stat(daemonsetScript); err == nil {
		return true
	}
	return false
}

// initOVNKubernetesSubmodule initializes and updates the ovn-kubernetes git submodule
func initOVNKubernetesSubmodule(projectRoot string) error {
	fmt.Println("Initializing ovn-kubernetes git submodule...")

	initCmd := exec.Command("git", "submodule", "init", "ovn-kubernetes")
	initCmd.Dir = projectRoot
	initCmd.Stdout = os.Stdout
	initCmd.Stderr = os.Stderr
	if err := initCmd.Run(); err != nil {
		return fmt.Errorf("failed to initialize submodule: %w", err)
	}

	updateCmd := exec.Command("git", "submodule", "update", "--init", "ovn-kubernetes")
	updateCmd.Dir = projectRoot
	updateCmd.Stdout = os.Stdout
	updateCmd.Stderr = os.Stderr
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("failed to update submodule: %w", err)
	}

	fmt.Println("✓ ovn-kubernetes submodule initialized")
	return nil
}

// EnsureOVNKubernetesSource ensures the ovn-kubernetes source code is available.
// It first tries to initialize the git submodule if it exists but is empty.
// If submodule initialization fails or the directory doesn't exist, it clones the repository.
func EnsureOVNKubernetesSource() (string, error) {
	ovnPath, err := GetOVNKubernetesPath()
	if err != nil {
		return "", fmt.Errorf("failed to get OVN-Kubernetes path: %w", err)
	}

	projectRoot, err := GetProjectRoot()
	if err != nil {
		return "", fmt.Errorf("failed to get project root: %w", err)
	}

	// Check if directory exists and is populated
	if _, err := os.Stat(ovnPath); err == nil {
		if isOVNKubernetesPopulated(ovnPath) {
			fmt.Printf("OVN-Kubernetes source found at %s\n", ovnPath)
			return ovnPath, nil
		}

		// Directory exists but is empty (uninitialized submodule)
		fmt.Println("OVN-Kubernetes directory exists but appears empty (uninitialized submodule)")
		if err := initOVNKubernetesSubmodule(projectRoot); err != nil {
			fmt.Printf("Warning: Failed to initialize submodule: %v\n", err)
			fmt.Println("Attempting to clone repository directly...")

			// Remove the empty directory and clone fresh
			if err := os.RemoveAll(ovnPath); err != nil {
				return "", fmt.Errorf("failed to remove empty ovn-kubernetes directory: %w", err)
			}
		} else {
			// Submodule initialized successfully
			if isOVNKubernetesPopulated(ovnPath) {
				return ovnPath, nil
			}
			return "", fmt.Errorf("submodule initialized but content still missing")
		}
	}

	// Directory doesn't exist or was removed - try submodule init first, then clone as fallback
	gitDir := filepath.Join(projectRoot, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		// We're in a git repository, try submodule init
		if err := initOVNKubernetesSubmodule(projectRoot); err == nil {
			if isOVNKubernetesPopulated(ovnPath) {
				return ovnPath, nil
			}
		}
		fmt.Println("Submodule initialization failed, falling back to clone...")
	}

	fmt.Printf("OVN-Kubernetes not found, cloning from %s:master...\n", DefaultOVNRepoURL)
	cmd := exec.Command("git", "clone", "--branch", "master", DefaultOVNRepoURL, ovnPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to clone OVN-Kubernetes repository: %w", err)
	}

	fmt.Printf("✓ OVN-Kubernetes cloned to %s\n", ovnPath)
	return ovnPath, nil
}
