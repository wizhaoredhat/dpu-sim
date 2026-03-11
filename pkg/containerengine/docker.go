package containerengine

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/wizhao/dpu-sim/pkg/platform"
)

// DockerEngine is the Docker-backed Engine implementation.
type DockerEngine struct {
	*baseEngine
}

func NewDockerEngine(exec platform.CommandExecutor) *DockerEngine {
	return &DockerEngine{
		baseEngine: &baseEngine{
			name: EngineDocker,
			bin:  "docker",
			exec: exec,
		},
	}
}

func (e *DockerEngine) Push(ctx context.Context, imageRef string, opts PushOptions) error {
	if opts.Insecure {
		registry, ok := registryFromImageRef(imageRef)
		if !ok {
			return fmt.Errorf("docker insecure push requires an explicit registry in image reference %q", imageRef)
		}

		allowed, err := e.isAllowedInsecureRegistry(registry)
		if err != nil {
			return fmt.Errorf("failed to verify docker insecure registries: %w", err)
		}
		if !allowed {
			return fmt.Errorf("docker daemon does not allow insecure registry %q; add it to insecure-registries and restart docker", registry)
		}
	}
	return e.baseEngine.push(ctx, imageRef, opts, "")
}

func (e *DockerEngine) TryRepairRunContainerFailure(_ context.Context, runErr error) (bool, error) {
	if runErr == nil {
		return false, nil
	}
	errText := strings.ToLower(runErr.Error())
	if !strings.Contains(errText, "unable to enable dnat rule") &&
		!strings.Contains(errText, "no chain/target/match by that name") &&
		!strings.Contains(errText, "driver failed programming external connectivity") {
		return false, nil
	}

	cmds := []string{
		"sudo systemctl restart docker",
		"systemctl restart docker",
	}

	var errs []string
	for _, cmd := range cmds {
		_, stderr, err := e.exec.Execute(cmd)
		if err == nil {
			_, _, infoErr := e.exec.Execute("docker info >/dev/null 2>&1")
			if infoErr == nil {
				return true, nil
			}
			errs = append(errs, fmt.Sprintf("%s succeeded but docker info failed: %v", cmd, infoErr))
			continue
		}
		errs = append(errs, fmt.Sprintf("%s failed: %v (%s)", cmd, err, strings.TrimSpace(stderr)))
	}

	return false, fmt.Errorf("%s", strings.Join(errs, "; "))
}

type dockerRegistryConfig struct {
	IndexConfigs          map[string]dockerRegistryIndexConfig `json:"IndexConfigs"`
	InsecureRegistryCIDRs []string                             `json:"InsecureRegistryCIDRs"`
}

type dockerRegistryIndexConfig struct {
	Secure bool `json:"Secure"`
}

func (e *DockerEngine) isAllowedInsecureRegistry(registry string) (bool, error) {
	stdout, _, err := e.exec.Execute("docker info --format '{{json .RegistryConfig}}'")
	if err != nil {
		return false, err
	}

	var cfg dockerRegistryConfig
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &cfg); err != nil {
		return false, fmt.Errorf("failed to parse docker registry config: %w", err)
	}

	targetHost := registryHost(registry)
	for name, idx := range cfg.IndexConfigs {
		if !idx.Secure && registryMatchesIndexEntry(name, registry) {
			return true, nil
		}
	}

	ip := net.ParseIP(strings.Trim(targetHost, "[]"))
	for _, cidr := range cfg.InsecureRegistryCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ip != nil && network.Contains(ip) {
			return true, nil
		}
	}

	return false, nil
}

func registryFromImageRef(imageRef string) (string, bool) {
	parts := strings.Split(imageRef, "/")
	if len(parts) < 2 {
		return "", false
	}

	registry := parts[0]
	if strings.Contains(registry, ".") || strings.Contains(registry, ":") || strings.EqualFold(registry, "localhost") {
		return registry, true
	}

	return "", false
}

func registryHost(registry string) string {
	if strings.HasPrefix(registry, "[") {
		host, _, err := net.SplitHostPort(registry)
		if err == nil {
			return strings.ToLower(host)
		}
		return strings.ToLower(registry)
	}

	if strings.Count(registry, ":") > 1 {
		return strings.ToLower(registry)
	}

	host, _, err := net.SplitHostPort(registry)
	if err == nil {
		return strings.ToLower(host)
	}

	return strings.ToLower(registry)
}

func registryMatchesIndexEntry(entry, target string) bool {
	normEntry := strings.ToLower(entry)
	normTarget := strings.ToLower(target)
	if normEntry == normTarget {
		return true
	}

	if isHostOnlyRegistryEntry(normEntry) && registryHost(normTarget) == normEntry {
		return true
	}

	return false
}

func isHostOnlyRegistryEntry(registry string) bool {
	if strings.HasPrefix(registry, "[") {
		_, _, err := net.SplitHostPort(registry)
		return err != nil
	}

	if strings.Count(registry, ":") > 1 {
		return true
	}

	_, _, err := net.SplitHostPort(registry)
	return err != nil
}
