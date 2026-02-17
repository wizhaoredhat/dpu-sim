// Package registry manages a local Docker registry container that is accessible
// by nodes in both VM and Kind deployment modes. It handles the lifecycle of the
// registry container (start/stop) and provides methods to tag and push images.
//
// The ImageLoader interface provides a shared abstraction for making container
// images available to cluster nodes, regardless of whether the deployment uses
// Kind or VMs. The Manager type implements ImageLoader using a local Docker
// registry as the backing store.
package registry

import (
	"fmt"
	"strings"
	"time"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

// ImageLoader is the interface for making container images available to
// cluster nodes. Implementations may use a local registry, direct image
// loading (e.g. kind load docker-image), or other mechanisms.
//
// The Manager type implements this interface using a local Docker registry.
type ImageLoader interface {
	// LoadImage makes a locally-built image available to cluster nodes.
	// localImage is the image:tag as it exists in the local Docker daemon
	// (e.g. "ovn-kube-fedora:dpu-sim").
	// tag is the desired name:tag in the target location (e.g. "ovn-kube:dpu-sim").
	// Returns the image reference that Kubernetes manifests should use when
	// referencing this image.
	LoadImage(localImage, tag string) (imageRef string, err error)
}

// BuildFunc builds a container image for a registry container config.
// Returns the local image name that was built (e.g. "ovn-kube:dpu-sim").
type BuildFunc func(container config.RegistryContainerConfig) (localImage string, err error)

// Manager manages the local Docker registry lifecycle and implements
// the ImageLoader interface.
type Manager struct {
	config *config.Config
	exec   platform.CommandExecutor
}

// Ensure Manager implements ImageLoader at compile time.
var _ ImageLoader = (*Manager)(nil)

// NewManager creates a new registry Manager
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		config: cfg,
		exec:   platform.NewLocalExecutor(),
	}
}

// GetAddress returns the registry address accessible from the Docker host
// (e.g. "localhost:5000").
func (m *Manager) GetAddress() string {
	return m.config.GetRegistryEndpoint()
}

// Start starts the local Docker registry container. If a registry container
// is already running, it is left as-is. If a stopped container exists, it is
// removed and recreated.
func (m *Manager) Start() error {
	log.Info("Starting local container registry...")

	containerName := m.config.GetRegistryContainerName()

	// Check if registry container already exists
	stdout, _, err := m.exec.Execute(
		fmt.Sprintf("docker inspect -f '{{.State.Running}}' %s 2>/dev/null", containerName))
	if err == nil {
		running := strings.TrimSpace(stdout)
		if running == "true" {
			log.Info("Registry container %s is already running", containerName)
			return nil
		}
		// Container exists but is not running — remove it so we can recreate
		log.Info("Removing stopped registry container %s...", containerName)
		m.exec.Execute(fmt.Sprintf("docker rm -f %s", containerName))
	}

	// Start the registry container
	cmd := fmt.Sprintf(
		"docker run -d --restart=always -p %s:5000 --network bridge --name %s %s",
		m.config.GetRegistryPort(), m.config.GetRegistryContainerName(), m.config.GetRegistryImage(),
	)
	if err := m.exec.RunCmd(log.LevelInfo, "sh", "-c", cmd); err != nil {
		return fmt.Errorf("failed to start registry container: %w", err)
	}

	// Wait a moment for the registry to become ready
	if err := m.waitForReady(30 * time.Second); err != nil {
		return fmt.Errorf("registry did not become ready: %w", err)
	}

	log.Info("✓ Local registry started at %s", m.GetAddress())
	return nil
}

// LoadImage implements the ImageLoader interface. It tags the local image and
// pushes it to the local registry, then returns the image reference that
// Kubernetes manifests should use to pull the image.
func (m *Manager) LoadImage(localImage, tag string) (string, error) {
	if err := m.TagAndPush(localImage, tag); err != nil {
		return "", fmt.Errorf("failed to load image into registry: %w", err)
	}
	return m.GetImageRef(tag), nil
}

// SetupAll starts the local registry, builds all configured container images
// using the provided build function, and pushes them to the registry.
// This is the main orchestration entry point for registry setup.
func (m *Manager) SetupAll(buildFunc BuildFunc) error {
	log.Info("\n=== Setting up Local Container Registry ===")

	if err := m.Start(); err != nil {
		return fmt.Errorf("failed to start registry: %w", err)
	}

	for _, container := range m.config.Registry.Containers {
		localImage, err := buildFunc(container)
		if err != nil {
			return fmt.Errorf("failed to build image for container %s: %w", container.Name, err)
		}

		if _, err := m.LoadImage(localImage, container.Tag); err != nil {
			return fmt.Errorf("failed to push container %s to registry: %w", container.Name, err)
		}
	}

	log.Info("✓ Registry setup complete")
	return nil
}

// ConnectToKindNetwork connects the registry container to the "kind" Docker
// network so that Kind nodes can reach it by container name.
func (m *Manager) ConnectToKindNetwork() error {
	// Check if already connected
	containerName := m.config.GetRegistryContainerName()

	stdout, _, _ := m.exec.Execute(
		fmt.Sprintf("docker inspect -f '{{index .NetworkSettings.Networks \"kind\"}}' %s 2>/dev/null", containerName))
	if strings.TrimSpace(stdout) != "" && strings.TrimSpace(stdout) != "<no value>" {
		log.Debug("Registry already connected to kind network")
		return nil
	}

	log.Info("Connecting registry to kind Docker network...")
	if err := m.exec.RunCmd(log.LevelDebug, "docker", "network", "connect", "kind", containerName); err != nil {
		return fmt.Errorf("failed to connect registry to kind network: %w", err)
	}

	log.Info("✓ Registry connected to kind network")
	return nil
}

// Stop stops and removes the local registry container
func (m *Manager) Stop() error {
	log.Info("Stopping local container registry...")

	if err := m.exec.RunCmd(log.LevelDebug, "docker", "rm", "-f", m.config.GetRegistryContainerName()); err != nil {
		log.Debug("Note: failed to remove registry container (may not exist): %v", err)
	}

	log.Info("✓ Local registry stopped")
	return nil
}

// TagAndPush tags a local image and pushes it to the local registry.
// localImage is the image:tag as it exists locally (e.g. "ovn-kube-fedora:dpu-sim").
// registryTag is the desired name:tag in the registry (e.g. "ovn-kube:dpu-sim").
func (m *Manager) TagAndPush(localImage, registryTag string) error {
	registryRef := fmt.Sprintf("%s/%s", m.GetAddress(), registryTag)

	log.Info("Tagging %s -> %s", localImage, registryRef)
	if err := m.exec.RunCmd(log.LevelDebug, "docker", "tag", localImage, registryRef); err != nil {
		return fmt.Errorf("failed to tag image %s as %s: %w", localImage, registryRef, err)
	}

	log.Info("Pushing %s to local registry...", registryRef)
	if err := m.exec.RunCmd(log.LevelInfo, "docker", "push", "--tls-verify=false", registryRef); err != nil {
		return fmt.Errorf("failed to push image %s: %w", registryRef, err)
	}

	log.Info("✓ Pushed %s to local registry", registryTag)
	return nil
}

// GetImageRef returns the full image reference for pulling from the local
// registry (e.g. "localhost:5000/ovn-kube:dpu-sim").
func (m *Manager) GetImageRef(registryTag string) string {
	return fmt.Sprintf("%s/%s", m.GetAddress(), registryTag)
}

// waitForReady waits for the registry to respond to HTTP requests
func (m *Manager) waitForReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, _, err := m.exec.Execute(
			fmt.Sprintf("curl -sf http://%s/v2/ >/dev/null 2>&1", m.GetAddress()))
		if err == nil {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("registry at %s did not respond within %s", m.GetAddress(), timeout)
}
