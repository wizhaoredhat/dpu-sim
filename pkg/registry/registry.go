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
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/containerengine"
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
type RegistryManager struct {
	config *config.Config
	exec   platform.CommandExecutor
	engine containerengine.Engine
}

// Ensure Manager implements ImageLoader at compile time.
var _ ImageLoader = (*RegistryManager)(nil)

// NewManager creates a new registry Manager
func NewRegistryManager(cfg *config.Config) (*RegistryManager, error) {
	exec := platform.NewLocalExecutor()
	engine, err := containerengine.NewProjectEngine(exec)
	if err != nil {
		return nil, err
	}
	return NewRegistryManagerWithRuntime(cfg, exec, engine), nil
}

// NewRegistryManagerWithRuntime creates a new registry manager with injected
// command executor and container engine.
func NewRegistryManagerWithRuntime(
	cfg *config.Config,
	exec platform.CommandExecutor,
	engine containerengine.Engine,
) *RegistryManager {
	return &RegistryManager{
		config: cfg,
		exec:   exec,
		engine: engine,
	}
}

// Start starts the local Docker registry container. If a registry container
// is already running, it is left as-is. If a stopped container exists, it is
// removed and recreated.
func (m *RegistryManager) Start() error {
	log.Info("Starting local container registry...")

	containerName := m.config.GetRegistryContainerName()
	ctx := context.Background()

	// Check if registry container already exists.
	state, err := m.engine.InspectContainerState(ctx, containerName)
	if err != nil {
		return fmt.Errorf("failed to inspect registry container state: %w", err)
	}
	if state.Exists {
		if state.Running {
			log.Info("Registry container %s is already running", containerName)
			return nil
		}
		// Container exists but is not running — remove it so we can recreate
		log.Info("Removing stopped registry container %s...", containerName)
		_ = m.engine.RemoveContainer(ctx, containerName, true)
	}

	if err := m.engine.RunContainer(ctx, containerengine.RunContainerOptions{
		Name:    m.config.GetRegistryContainerName(),
		Image:   m.config.GetRegistryImage(),
		Detach:  true,
		Restart: "always",
		Network: "bridge",
		Publish: []string{fmt.Sprintf("%s:5000", m.config.GetRegistryPort())},
	}); err != nil {
		return fmt.Errorf("failed to start registry container: %w", err)
	}

	// Wait a moment for the registry to become ready
	if err := m.waitForReady(30 * time.Second); err != nil {
		return fmt.Errorf("registry did not become ready: %w", err)
	}

	log.Info("Local registry started at %s", m.config.GetRegistryLocalEndpoint())
	return nil
}

// LoadImage implements the ImageLoader interface. It tags the local image and
// pushes it to the local registry, then returns the image reference that
// Kubernetes manifests should use to pull the image.
func (m *RegistryManager) LoadImage(localImage, tag string) (string, error) {
	imageRef := m.config.GetRegistryLocalImageRef(tag)
	if err := m.tagAndPush(localImage, imageRef); err != nil {
		return "", fmt.Errorf("failed to load image into registry: %w", err)
	}
	return imageRef, nil
}

// SetupAll starts the local registry, builds all configured container images
// using the provided build function, and pushes them to the registry.
// This is the main orchestration entry point for registry setup.
func (m *RegistryManager) SetupAll(buildFunc BuildFunc) error {
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

	log.Info("Registry setup complete")
	return nil
}

// ConnectToKindNetwork connects the registry container to the "kind" Docker
// network so that Kind nodes can reach it by container name.
func (m *RegistryManager) ConnectToKindNetwork() error {
	containerName := m.config.GetRegistryContainerName()
	ctx := context.Background()

	// Check if already connected by parsing JSON (works with both Docker and Podman)
	if ip, err := m.GetKindNetworkIP(); err == nil && ip != "" {
		log.Debug("Registry already connected to kind network (IP: %s)", ip)
		return nil
	}

	log.Info("Connecting registry to kind Docker network...")

	if err := m.engine.ConnectNetwork(ctx, "kind", containerName); err != nil {
		return fmt.Errorf("failed to connect registry to kind network: %w", err)
	}

	log.Info("Registry connected to kind network")
	return nil
}

// GetKindNetworkIP returns the registry container's IP address on the
// "kind" Docker network. Must be called after ConnectToKindNetwork.
// Uses JSON output + jq-style parsing to work with both Docker and Podman.
func (m *RegistryManager) GetKindNetworkIP() (string, error) {
	containerName := m.config.GetRegistryContainerName()
	networks, err := m.engine.InspectContainerNetworks(context.Background(), containerName)
	if err != nil {
		return "", fmt.Errorf("failed to inspect registry container: %w", err)
	}

	log.Debug("Registry networks: %v", networks)

	// Try exact match first, then substring match for Podman compatibility
	if net, ok := networks["kind"]; ok && net.IPAddress != "" {
		return net.IPAddress, nil
	}
	for name, net := range networks {
		if strings.Contains(name, "kind") && net.IPAddress != "" {
			log.Debug("Found kind network under key %q", name)
			return net.IPAddress, nil
		}
	}

	return "", fmt.Errorf("registry container %s is not connected to the kind network", containerName)
}

// Stop stops and removes the local registry container
func (m *RegistryManager) Stop() error {
	log.Info("Stopping local container registry...")

	containerName := m.config.GetRegistryContainerName()

	if err := m.engine.RemoveContainer(context.Background(), containerName, true); err != nil {
		log.Debug("Failed to remove registry container %s (may not exist): %v", containerName, err)
	}

	log.Info("Local registry %s stopped", containerName)
	return nil
}

// tagAndPush tags a local image and pushes it to the local registry.
// localImage is the image:tag as it exists locally (e.g. "ovn-kube-fedora:dpu-sim").
// registryRef is the desired image reference in the registry (e.g. "localhost:5000/ovn-kube:dpu-sim").
func (m *RegistryManager) tagAndPush(localImage, registryRef string) error {
	log.Info("Tagging %s -> %s", localImage, registryRef)
	ctx := context.Background()
	if err := m.engine.Tag(ctx, localImage, registryRef); err != nil {
		return fmt.Errorf("failed to tag image %s as %s: %w", localImage, registryRef, err)
	}

	log.Info("Pushing %s to local registry...", registryRef)
	if err := m.engine.Push(ctx, registryRef, containerengine.PushOptions{Insecure: true}); err != nil {
		return fmt.Errorf("failed to push image %s: %w", registryRef, err)
	}

	log.Info("Pushed %s to local registry", registryRef)
	return nil
}

// waitForReady waits for the registry to respond to HTTP requests
func (m *RegistryManager) waitForReady(timeout time.Duration) error {
	endpoint := m.config.GetRegistryLocalEndpoint()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, _, err := m.exec.Execute(
			fmt.Sprintf("curl -sf http://%s/v2/ >/dev/null 2>&1", endpoint))
		if err == nil {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("registry at %s did not respond within %s", endpoint, timeout)
}
