package vm

import "testing"

// TestSetLibvirtFirewallBackendAddsSettingWhenMissing verifies the helper
// appends firewall_backend when the key is not present in the input config.
func TestSetLibvirtFirewallBackendAddsSettingWhenMissing(t *testing.T) {
	t.Parallel()

	input := "# libvirt network config\n"
	updated, changed := setLibvirtFirewallBackend(input, "nftables")

	if !changed {
		t.Fatalf("expected change when setting is missing")
	}
	want := "# libvirt network config\n\nfirewall_backend = \"nftables\"\n"
	if updated != want {
		t.Fatalf("unexpected config\n got: %q\nwant: %q", updated, want)
	}
}

// TestSetLibvirtFirewallBackendReplacesExistingSetting verifies an existing
// firewall_backend value is rewritten to the requested backend.
func TestSetLibvirtFirewallBackendReplacesExistingSetting(t *testing.T) {
	t.Parallel()

	input := "# libvirt network config\nfirewall_backend = \"iptables\"\n"
	updated, changed := setLibvirtFirewallBackend(input, "nftables")

	if !changed {
		t.Fatalf("expected change when backend differs")
	}
	want := "# libvirt network config\nfirewall_backend = \"nftables\"\n"
	if updated != want {
		t.Fatalf("unexpected config\n got: %q\nwant: %q", updated, want)
	}
}

// TestSetLibvirtFirewallBackendNoopWhenAlreadySet verifies the helper leaves
// config content unchanged when the desired backend is already configured.
func TestSetLibvirtFirewallBackendNoopWhenAlreadySet(t *testing.T) {
	t.Parallel()

	input := "firewall_backend = \"nftables\"\n"
	updated, changed := setLibvirtFirewallBackend(input, "nftables")

	if changed {
		t.Fatalf("expected no change when backend is already set")
	}
	if updated != input {
		t.Fatalf("expected unchanged config\n got: %q\nwant: %q", updated, input)
	}
}
