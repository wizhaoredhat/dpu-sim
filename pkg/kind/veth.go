package kind

import (
	"fmt"
	"strings"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

const (
	// Format strings for veth endpoint names on the host namespace (before
	// they are moved into containers and renamed).
	vethHostEndFmt = "host%d-eth0-%d" // host-side data veth: host{pairIdx}-eth0-{i}
	vethDpuEndFmt  = "dpu%d-rep0-%d"  // DPU-side data veth:  dpu{pairIdx}-rep0-{i}

	// Names used inside the containers after rename.
	hostDataIfFmt = "eth0-%d" // data interface in host container
	dpuDataIfFmt  = "rep0-%d" // data representor in DPU container
)

// SetupHostToDpuNetwork reads the HostToDpu network config and creates veth
// channels between host and DPU Kind containers (which may be in different clusters).
func (m *KindManager) SetupHostToDpuNetwork() error {
	h2dCfg := m.config.GetHostToDpuNetwork()
	if h2dCfg == nil {
		return nil
	}

	pairs := m.config.GetKindHostDPUPairs()
	if len(pairs) == 0 {
		log.Warn("HostToDpu network configured but no dpu-host/dpu worker pairs found in Kind node config")
		return nil
	}

	numPairs := m.config.GetHostToDpuNumPairs()
	hostExec := platform.NewLocalExecutor()
	m.CleanupVethTopology(hostExec, pairs, numPairs)
	return m.CreateVethTopology(hostExec, pairs, numPairs)
}

// CleanupVethTopology removes leftover veth interfaces from a previous incomplete run.
// When veths are in kind containers, they are automatically cleaned up by the kind delete command.
// When a container is destroyed, its network namespace is torn down by the kernel which automatically
// deletes the interfaces in that namespace.
func (m *KindManager) CleanupVethTopology(cmdExec platform.CommandExecutor, pairs []config.KindHostDPUPair, numPairs int) {
	for pairIdx := range pairs {
		for i := 0; i < numPairs; i++ {
			cmdExec.RunCmd(log.LevelDebug, "sudo", "ip", "link", "delete", fmt.Sprintf(vethHostEndFmt, pairIdx, i))
		}
	}
}

// CreateVethTopology creates veth pair channels for each host-DPU pair.
// numPairs controls how many data channels are created per pair.
//
// For each pair the following interfaces are created:
//
// Host container:  eth0-0 … eth0-(numPairs-1) e.g. eth0-0, eth0-1, …, eth0-15
// DPU  container:  rep0-0 … rep0-(numPairs-1) e.g. rep0-0, rep0-1, …, rep0-15
//
// A separate management veth pair is also created for each pair:
//
// Host container:  pf     (takes over the Kind IP from eth0)
// DPU  container:  pfrep
func (m *KindManager) CreateVethTopology(cmdExec platform.CommandExecutor, pairs []config.KindHostDPUPair, numPairs int) error {
	for pairIdx, pair := range pairs {
		log.Info("Setting up veth topology for pair %d: %s <-> %s (%d data channels)",
			pairIdx, pair.HostContainer, pair.DPUContainer, numPairs)

		hostPID, err := getContainerPID(cmdExec, pair.HostContainer)
		if err != nil {
			return fmt.Errorf("failed to get PID for host container %s: %w", pair.HostContainer, err)
		}
		dpuPID, err := getContainerPID(cmdExec, pair.DPUContainer)
		if err != nil {
			return fmt.Errorf("failed to get PID for DPU container %s: %w", pair.DPUContainer, err)
		}

		hostContainerExec := platform.NewDockerExecutor(pair.HostContainer)
		dpuContainerExec := platform.NewDockerExecutor(pair.DPUContainer)

		if err := createDataVeths(cmdExec, hostContainerExec, dpuContainerExec, pairIdx, hostPID, dpuPID, numPairs); err != nil {
			return fmt.Errorf("failed to create data veths for pair %d: %w", pairIdx, err)
		}
	}

	log.Info("✓ Veth topology created for %d host-DPU pairs (%d data channels each)", len(pairs), numPairs)
	return nil
}

// createDataVeths creates numPairs veth pairs and moves them into the
// containers. Host side is renamed to eth0-{i}, DPU side to rep0-{i}.
func createDataVeths(
	hostExec platform.CommandExecutor,
	hostContainerExec, dpuContainerExec platform.CommandExecutor,
	pairIdx int, hostPID, dpuPID string, numPairs int,
) error {
	for i := 0; i < numPairs; i++ {
		hostEnd := fmt.Sprintf(vethHostEndFmt, pairIdx, i)
		dpuEnd := fmt.Sprintf(vethDpuEndFmt, pairIdx, i)

		if err := hostExec.RunCmd(log.LevelDebug, "sudo", "ip", "link", "add", hostEnd, "type", "veth", "peer", "name", dpuEnd); err != nil {
			return fmt.Errorf("failed to create veth pair %s <-> %s: %w", hostEnd, dpuEnd, err)
		}

		if err := hostExec.RunCmd(log.LevelDebug, "sudo", "ip", "link", "set", hostEnd, "netns", hostPID); err != nil {
			return fmt.Errorf("failed to move %s to host container: %w", hostEnd, err)
		}
		if err := hostExec.RunCmd(log.LevelDebug, "sudo", "ip", "link", "set", dpuEnd, "netns", dpuPID); err != nil {
			return fmt.Errorf("failed to move %s to DPU container: %w", dpuEnd, err)
		}

		hostTarget := fmt.Sprintf(hostDataIfFmt, i)
		if err := hostContainerExec.RunCmd(log.LevelDebug, "ip", "link", "set", hostEnd, "name", hostTarget); err != nil {
			return fmt.Errorf("failed to rename %s to %s in host container: %w", hostEnd, hostTarget, err)
		}
		if err := hostContainerExec.RunCmd(log.LevelDebug, "ip", "link", "set", hostTarget, "up"); err != nil {
			return fmt.Errorf("failed to bring up %s in host container: %w", hostTarget, err)
		}

		dpuTarget := fmt.Sprintf(dpuDataIfFmt, i)
		if err := dpuContainerExec.RunCmd(log.LevelDebug, "ip", "link", "set", dpuEnd, "name", dpuTarget); err != nil {
			return fmt.Errorf("failed to rename %s to %s in DPU container: %w", dpuEnd, dpuTarget, err)
		}
		if err := dpuContainerExec.RunCmd(log.LevelDebug, "ip", "link", "set", dpuTarget, "up"); err != nil {
			return fmt.Errorf("failed to bring up %s in DPU container: %w", dpuTarget, err)
		}
	}
	return nil
}

func getContainerPID(cmdExec platform.CommandExecutor, container string) (string, error) {
	stdout, _, err := cmdExec.Execute(fmt.Sprintf("docker inspect --format '{{.State.Pid}}' %s", container))
	if err != nil {
		return "", fmt.Errorf("failed to inspect container %s: %w", container, err)
	}
	pid := strings.TrimSpace(stdout)
	if pid == "" || pid == "0" {
		return "", fmt.Errorf("container %s has no running PID", container)
	}
	return pid, nil
}
