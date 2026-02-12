// Package platform provides utilities for Linux distribution detection and management
package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/log"
)

// getProjectRoot returns the root directory of the dpu-sim project
func getProjectRoot() (string, error) {
	// Get the directory of the current source file
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("failed to get current file path")
	}

	// Navigate from pkg/linux/deps.go to project root (2 levels up)
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(filename)))
	return projectRoot, nil
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

// UnsupportedPackageManager returns an error for an unsupported package manager
func UnsupportedPackageManager(distro *Distro) error {
	return fmt.Errorf("unsupported package manager: %s", distro.PackageManager)
}

// UnsupportedArchitecture returns an error for an unsupported architecture
func UnsupportedArchitecture(distro *Distro) error {
	return fmt.Errorf("unsupported architecture: %s", distro.Architecture)
}

// checkDependency checks if a single dependency is installed
func checkDependency(cmdExec CommandExecutor, dep Dependency, distro *Distro, cfg *config.Config) DependencyResult {
	result := DependencyResult{
		Name:      dep.Name,
		Installed: false,
	}

	// If CheckCmd is empty, use CheckFunc (for libraries without executables)
	if len(dep.CheckCmd) == 0 {
		if dep.CheckFunc == nil {
			result.Error = fmt.Errorf("no check command or check function defined for dependency %s", dep.Name)
			return result
		}
		if err := dep.CheckFunc(cmdExec, distro, cfg, &dep); err != nil {
			result.Error = err
			return result
		}
		result.Installed = true
		return result
	}

	// Build command string from CheckCmd
	checkCmd := strings.Join(dep.CheckCmd, " ")
	stdout, stderr, err := cmdExec.Execute(checkCmd)
	result.Output = stdout + stderr
	if err != nil {
		result.Error = err
		return result
	}

	result.Installed = true
	return result
}

// installDependency attempts to install a dependency using its install function
func installDependency(cmdExec CommandExecutor, dep Dependency, distro *Distro, cfg *config.Config) error {
	if dep.InstallFunc == nil {
		return fmt.Errorf("no install function defined for %s", dep.Name)
	}

	log.Info("Installing %s for %s on %s...", dep.Name, distro.ID, cmdExec.String())

	if err := dep.InstallFunc(cmdExec, distro, cfg, &dep); err != nil {
		return fmt.Errorf("failed to install %s: %w. Needed for %s", dep.Name, err, dep.Reason)
	}

	log.Info("✓ %s installed", dep.Name)
	return nil
}

// EnsureDependenciesWithExecutor checks and installs dependencies using the provided executor
// This allows installing dependencies on remote machines (via SSH) or in containers (via Docker)
// exec is the CommandExecutor that determines where commands are run
// cfg provides configuration including version information for dependencies
// Returns an error if any dependency cannot be installed
func EnsureDependenciesWithExecutor(cmdExec CommandExecutor, deps []Dependency, cfg *config.Config) error {
	log.Debug("Checking dependencies on %s...", cmdExec.String())

	// Ensure ~/.local/bin is in PATH for pip user installs (only affects local executor)
	ensureLocalBinInPath()

	// Detect distro using the executor
	distro, err := cmdExec.GetDistro()
	if err != nil {
		return fmt.Errorf("failed to detect distribution on %s: %w", cmdExec.String(), err)
	}
	log.Info("✓ Detected Linux distribution: %s %s (package manager: %s, architecture: %s)", distro.ID, distro.VersionID, distro.PackageManager, distro.Architecture)

	var missing []Dependency
	for _, dep := range deps {
		result := checkDependency(cmdExec, dep, distro, cfg)
		if result.Installed {
			log.Info("✓ %s is installed", dep.Name)
		} else {
			log.Debug("✗ %s is not installed", dep.Name)
			missing = append(missing, dep)
		}
	}

	if len(missing) > 0 {
		var names []string
		for _, dep := range missing {
			names = append(names, dep.Name)
		}
		log.Info("Installing missing dependencies: %s", strings.Join(names, ", "))
		for _, dep := range missing {
			if err := installDependency(cmdExec, dep, distro, cfg); err != nil {
				return fmt.Errorf("failed to install dependency %s: %w", dep.Name, err)
			}

			result := checkDependency(cmdExec, dep, distro, cfg)
			if !result.Installed {
				return fmt.Errorf("dependency %s was installed but verification failed", dep.Name)
			}
		}
	}

	log.Info("✓ All dependencies are available")
	return nil
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
func isOVNKubernetesPopulated(cmdExec CommandExecutor, ovnPath string) bool {
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
func initOVNKubernetesSubmodule(cmdExec CommandExecutor, projectRoot string) error {
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
func EnsureOVNKubernetesSource(cmdExec CommandExecutor) (string, error) {
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
func BuildOVNKubernetesImage(cmdExec CommandExecutor, imageName string, ovnGitRef string) error {
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

	// Write a .dockerignore to the build context to reduce its size and improve
	// layer cache stability. The COPY in the Dockerfile sends all files from the
	// build context; without this, docs/, test/, .github/, helm/, etc. are
	// included unnecessarily. Any change in those directories would also
	// invalidate the COPY layer cache and trigger a full Go rebuild.
	if err := writeDockerignore(cmdExec, ovnPath); err != nil {
		log.Warn("Warning: failed to write .dockerignore, build may be slower: %v", err)
	}
	defer cleanupDockerignore(cmdExec, ovnPath)

	// Detect architecture from the executor's target system
	targetArch, err := cmdExec.GetArchitecture()
	if err != nil {
		return fmt.Errorf("failed to detect architecture: %w", err)
	}
	arch := targetArch.GoArch()

	// Detect whether "docker" is actually podman (common on Fedora/RHEL)
	isPodman := isDockerPodman(cmdExec)

	// Build the Go builder image reference
	const goVersion = "1.24"
	goImage := fmt.Sprintf("quay.io/projectquay/golang:%s", goVersion)

	// Determine OVN_FROM: "koji" (pre-built RPMs) or "source" (build from git)
	ovnFrom := "koji"
	if ovnGitRef != "" {
		ovnFrom = "source"
	}

	args := []string{
		"build",
		"--build-arg", "BUILDER_IMAGE=" + goImage,
		"--build-arg", "OVN_FROM=" + ovnFrom,
		"--build-arg", "OVN_KUBERNETES_DIR=.",
		// Podman does not auto-populate BUILDPLATFORM/TARGETOS/TARGETARCH
		// the way Docker BuildKit does. Pass them explicitly so the
		// Dockerfile's cross-compilation logic works correctly.
		"--build-arg", "BUILDPLATFORM=linux/" + arch,
		"--build-arg", "TARGETOS=linux",
		"--build-arg", "TARGETARCH=" + arch,
		"--platform", "linux/" + arch,
		"-t", imageName,
		"-f", dockerfile,
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
			args = append(args, "--volume", cacheDir+":/root/.cache/go-build:Z")
		}
	}

	// When building OVN from source, resolve the git ref to a SHA and pass it
	if ovnGitRef != "" {
		ovnRepo := "https://github.com/ovn-org/ovn.git"
		sha, err := resolveGitRef(cmdExec, ovnRepo, ovnGitRef)
		if err != nil {
			return fmt.Errorf("failed to resolve OVN git ref %q: %w", ovnGitRef, err)
		}
		args = append(args,
			"--build-arg", "OVN_REPO="+ovnRepo,
			"--build-arg", "OVN_GITREF="+sha,
		)
	}

	// Build context is the ovn-kubernetes repo root
	args = append(args, ovnPath)

	log.Info("Building OVN-Kubernetes image %s (OVN_FROM=%s, arch=%s)...", imageName, ovnFrom, arch)

	if err := cmdExec.RunCmd(log.LevelInfo, "docker", args...); err != nil {
		return fmt.Errorf("failed to build OVN-Kubernetes image: %w", err)
	}

	log.Info("✓ OVN-Kubernetes image built successfully: %s", imageName)
	return nil
}

// isDockerPodman detects whether the "docker" command is actually podman.
// On Fedora/RHEL systems, podman is often aliased as docker.
func isDockerPodman(cmdExec CommandExecutor) bool {
	stdout, _, err := cmdExec.Execute("docker --version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(stdout), "podman")
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
func writeDockerignore(cmdExec CommandExecutor, buildContextDir string) error {
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
func cleanupDockerignore(cmdExec CommandExecutor, buildContextDir string) {
	dockerignorePath := filepath.Join(buildContextDir, ".dockerignore")
	if err := cmdExec.RemoveAll(dockerignorePath); err != nil {
		log.Debug("Note: failed to remove .dockerignore at %s: %v", dockerignorePath, err)
	}
}

// resolveGitRef resolves a git ref (branch, tag, or commit) to a full SHA using ls-remote.
func resolveGitRef(cmdExec CommandExecutor, repo, ref string) (string, error) {
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
