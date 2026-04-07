package vm

import (
	"errors"
	"strings"
	"testing"
)

// TestBuildBareMetalResetPhasesOrder verifies reset phases are emitted in the
// expected order so destructive cleanup happens before service reload and final
// validation.
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

// TestBuildBareMetalResetPhasesAreGeneric verifies generated reset scripts keep
// generic kube/ovs cleanup primitives and do not include environment-specific
// MCO/OpenShift-only commands.
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

// TestIsExpectedBootcApplyDisconnect verifies we only suppress errors that
// match expected SSH disconnect patterns caused by immediate reboot during
// bootc --apply.
func TestIsExpectedBootcApplyDisconnect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "ssh EOF", err: errors.New("EOF"), want: true},
		{name: "broken pipe", err: errors.New("write tcp: broken pipe"), want: true},
		{name: "connection reset", err: errors.New("read: connection reset by peer"), want: true},
		{name: "unexpected command failure", err: errors.New("exit status 1"), want: false},
		{name: "auth failure", err: errors.New("permission denied"), want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isExpectedBootcApplyDisconnect(tt.err)
			if got != tt.want {
				t.Fatalf("isExpectedBootcApplyDisconnect() = %v, want %v", got, tt.want)
			}
		})
	}
}
