package cni

import (
	"testing"

	"github.com/wizhao/dpu-sim/pkg/config"
)

func TestShouldUseWritableCNIBinDir(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{
			name: "kind uses default upstream cni paths",
			cfg: &config.Config{
				Kind: &config.KindConfig{
					Nodes: []config.KindNodeConfig{
						{Name: "cp", K8sCluster: "c", K8sRole: "control-plane"},
					},
				},
			},
			want: false,
		},
		{
			name: "vm mode uses default upstream cni paths",
			cfg: &config.Config{
				VMs: []config.VMConfig{{Name: "vm1"}},
			},
			want: false,
		},
		{
			name: "bare metal without bootc uses default upstream cni paths",
			cfg: &config.Config{
				BareMetal: []config.BareMetalConfig{{Name: "n1"}},
			},
			want: false,
		},
		{
			name: "operating_system image_ref uses writable cni bin",
			cfg: &config.Config{
				VMs:             []config.VMConfig{{Name: "vm1"}},
				OperatingSystem: config.OSConfig{ImageRef: "quay.io/example/os:latest"},
			},
			want: true,
		},
		{
			name: "bare metal bootc uses writable cni bin",
			cfg: &config.Config{
				BareMetal: []config.BareMetalConfig{{
					Name:  "n1",
					Bootc: &config.BareMetalBootcConfig{Enabled: true},
				}},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, err := NewCNIManager(tt.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got := m.shouldUseWritableCNIBinDir(); got != tt.want {
				t.Fatalf("shouldUseWritableCNIBinDir() = %v, want %v", got, tt.want)
			}
		})
	}
}
