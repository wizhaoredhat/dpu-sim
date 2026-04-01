package registry

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/containerengine"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

type fakeEngine struct {
	name             containerengine.Name
	state            containerengine.ContainerState
	runErrs          []error
	runCalls         int
	repairCalled     int
	repairResult     bool
	repairErr        error
	inspectStateErr  error
	removeCalledWith []string
}

func (f *fakeEngine) Name() containerengine.Name { return f.name }
func (f *fakeEngine) Build(context.Context, containerengine.BuildOptions) error {
	return nil
}
func (f *fakeEngine) Tag(context.Context, string, string) error { return nil }
func (f *fakeEngine) Push(context.Context, string, containerengine.PushOptions) error {
	return nil
}
func (f *fakeEngine) ImageExists(context.Context, string) bool { return false }
func (f *fakeEngine) RunContainer(context.Context, containerengine.RunContainerOptions) error {
	f.runCalls++
	if len(f.runErrs) == 0 {
		return nil
	}
	err := f.runErrs[0]
	f.runErrs = f.runErrs[1:]
	return err
}
func (f *fakeEngine) TryRepairRunContainerFailure(context.Context, error) (bool, error) {
	f.repairCalled++
	return f.repairResult, f.repairErr
}
func (f *fakeEngine) RemoveContainer(_ context.Context, name string, _ bool) error {
	f.removeCalledWith = append(f.removeCalledWith, name)
	return nil
}
func (f *fakeEngine) InspectContainerState(context.Context, string) (containerengine.ContainerState, error) {
	if f.inspectStateErr != nil {
		return containerengine.ContainerState{}, f.inspectStateErr
	}
	return f.state, nil
}
func (f *fakeEngine) InspectContainerNetworks(context.Context, string) (map[string]containerengine.NetworkEndpoint, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeEngine) ConnectNetwork(context.Context, string, string) error { return nil }

type fakeExec struct {
	commands []string
}

func (f *fakeExec) WaitUntilReady(time.Duration) error { return nil }
func (f *fakeExec) Execute(command string) (string, string, error) {
	f.commands = append(f.commands, command)
	if strings.HasPrefix(command, "curl -sf http://") {
		return "", "", nil
	}
	return "", "", nil
}
func (f *fakeExec) ExecuteWithTimeout(command string, timeout time.Duration) (string, string, error) {
	return f.Execute(command)
}
func (f *fakeExec) RunCmd(level log.Level, name string, args ...string) error { return nil }
func (f *fakeExec) RunCmdInDir(level log.Level, dir string, name string, args ...string) error {
	return nil
}
func (f *fakeExec) FileExists(path string) (bool, error) { return false, nil }
func (f *fakeExec) ReadFile(path string) ([]byte, error) { return nil, nil }
func (f *fakeExec) WriteFile(path string, content []byte, mode os.FileMode) error {
	return nil
}
func (f *fakeExec) RemoveAll(path string) error          { return nil }
func (f *fakeExec) GetDistro() (*platform.Distro, error) { return nil, nil }
func (f *fakeExec) GetArchitecture() (platform.Architecture, error) {
	return platform.X86_64, nil
}
func (f *fakeExec) String() string { return "fake" }

func (f *fakeExec) HasSudo() bool { return true }

func TestStartRepairsDockerFirewallAndRetriesOnDNATError(t *testing.T) {
	fEngine := &fakeEngine{
		name:         containerengine.EngineDocker,
		state:        containerengine.ContainerState{Exists: false, Running: false},
		runErrs:      []error{errors.New("Unable to enable DNAT rule: iptables: No chain/target/match by that name"), nil},
		repairResult: true,
	}
	fExec := &fakeExec{}
	m := NewRegistryManagerWithRuntime(&config.Config{}, fExec, fEngine)

	if err := m.Start(); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if fEngine.runCalls != 2 {
		t.Fatalf("expected 2 run attempts, got %d", fEngine.runCalls)
	}
	if fEngine.repairCalled != 1 {
		t.Fatalf("expected 1 repair attempt, got %d", fEngine.repairCalled)
	}
	if len(fEngine.removeCalledWith) != 1 || fEngine.removeCalledWith[0] != "dpu-sim-registry" {
		t.Fatalf("expected cleanup remove before retry, got: %#v", fEngine.removeCalledWith)
	}
}

func TestStartDoesNotRetryForPodmanOnDNATError(t *testing.T) {
	fEngine := &fakeEngine{
		name:    containerengine.EnginePodman,
		state:   containerengine.ContainerState{Exists: false, Running: false},
		runErrs: []error{errors.New("Unable to enable DNAT rule: iptables: No chain/target/match by that name")},
	}
	fExec := &fakeExec{}
	m := NewRegistryManagerWithRuntime(&config.Config{}, fExec, fEngine)

	err := m.Start()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if fEngine.runCalls != 1 {
		t.Fatalf("expected 1 run attempt for podman, got %d", fEngine.runCalls)
	}
	if fEngine.repairCalled != 1 {
		t.Fatalf("expected manager to call repair hook once, got %d", fEngine.repairCalled)
	}
	if len(fEngine.removeCalledWith) != 0 {
		t.Fatalf("did not expect container remove without repair, got: %#v", fEngine.removeCalledWith)
	}
}
