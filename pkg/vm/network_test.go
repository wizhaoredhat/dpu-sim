package vm

import "testing"

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
