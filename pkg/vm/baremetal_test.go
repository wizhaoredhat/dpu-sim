package vm

import (
	"strings"
	"testing"
)

func TestBuildBareMetalResetPhasesOrder(t *testing.T) {
	t.Parallel()

	phases := buildBareMetalResetPhases()
	if len(phases) != 5 {
		t.Fatalf("unexpected phase count: got %d, want 5", len(phases))
	}

	wantNames := []string{
		"stop kubernetes services",
		"cleanup kubernetes state",
		"ensure ovs prerequisites",
		"reload services",
		"validate reset state",
	}

	for i, want := range wantNames {
		if phases[i].name != want {
			t.Fatalf("unexpected phase name at index %d: got %q, want %q", i, phases[i].name, want)
		}
		if !strings.HasPrefix(phases[i].script, "set -e\n") {
			t.Fatalf("phase %q does not start with set -e", phases[i].name)
		}
	}
}

func TestBuildBareMetalResetPhasesAreGeneric(t *testing.T) {
	t.Parallel()

	phases := buildBareMetalResetPhases()
	combined := strings.Builder{}
	for _, phase := range phases {
		combined.WriteString(phase.script)
	}
	script := combined.String()

	mustContain := []string{
		"sudo kubeadm reset -f || true",
		"sudo rm -rf /etc/cni/net.d /var/lib/cni",
		"sudo rm -rf /etc/kubernetes /var/lib/kubelet",
		"sudo systemctl daemon-reload",
	}
	for _, token := range mustContain {
		if !strings.Contains(script, token) {
			t.Fatalf("expected reset script to contain %q", token)
		}
	}

	mustNotContain := []string{
		"/var/lib/mco",
		"mco-",
		"nodenet",
		"NetworkManager-clean-initrd-state",
		"wait-for-br-ex-up",
	}
	for _, token := range mustNotContain {
		if strings.Contains(script, token) {
			t.Fatalf("expected reset script to not contain %q", token)
		}
	}
}
