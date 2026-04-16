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
			name: "vm mode patches manifests to var lib cni bin",
			cfg: &config.Config{
				VMs: []config.VMConfig{{Name: "vm1"}},
			},
			want: true,
		},
		{
			name: "bare metal mode patches manifests to var lib cni bin",
			cfg: &config.Config{
				BareMetal: []config.BareMetalConfig{{Name: "n1"}},
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
