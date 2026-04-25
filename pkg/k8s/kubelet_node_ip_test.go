package k8s

import (
	"strings"
	"testing"
)

func TestMergeNodeIPIntoKubeadmArgsLine(t *testing.T) {
	const ip = "192.168.123.12"
	in := `KUBELET_KUBEADM_ARGS="--foo=1 --node-ip=192.168.120.25"
`
	out, changed, err := mergeNodeIPIntoKubeadmArgsLine(in, ip)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	if !strings.Contains(out, `--node-ip=`+ip) || strings.Contains(out, "192.168.120.25") {
		t.Fatalf("bad output: %q", out)
	}
	if strings.Count(out, "--node-ip=") != 1 {
		t.Fatalf("expected single --node-ip: %q", out)
	}
}

func TestMergeNodeIPIntoKubeadmArgsLineAppend(t *testing.T) {
	const ip = "192.168.123.12"
	in := `KUBELET_KUBEADM_ARGS="--cgroup-driver=systemd"
`
	out, changed, err := mergeNodeIPIntoKubeadmArgsLine(in, ip)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	if !strings.Contains(out, `--node-ip=`+ip) {
		t.Fatalf("bad output: %q", out)
	}
}

func TestMergeNodeIPIntoKubeadmArgsLineNoop(t *testing.T) {
	const ip = "192.168.123.12"
	in := `KUBELET_KUBEADM_ARGS="--x=1 --node-ip=` + ip + `"
`
	out, changed, err := mergeNodeIPIntoKubeadmArgsLine(in, ip)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatalf("unexpected change: %q", out)
	}
}
