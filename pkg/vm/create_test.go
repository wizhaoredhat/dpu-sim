package vm

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wizhao/dpu-sim/pkg/config"
)

func TestNvramPathForTemplatePreservesExtension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
		wantExt  string
	}{
		{name: "fd template", template: "/usr/share/edk2/aarch64/QEMU_VARS.fd", wantExt: ".fd"},
		{name: "qcow2 template", template: "/usr/share/edk2/aarch64/vars-template-pflash.qcow2", wantExt: ".qcow2"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := nvramPathForTemplate("vm-test", tt.template)
			if got := filepath.Ext(path); got != tt.wantExt {
				t.Fatalf("expected extension %q, got %q", tt.wantExt, got)
			}
		})
	}
}

// Verifies that firmware selection ignores undersized pflash and selects the first valid pair.
func TestFindAarch64UEFIFirmwareWithStat(t *testing.T) {
	t.Parallel()

	candidates := []firmwareCandidate{
		{"/firmware/small.fd", "/firmware/small-vars.fd"},
		{"/firmware/good.fd", "/firmware/good-vars.fd"},
	}

	mapFS := fstest.MapFS{
		"firmware/small.fd":      &fstest.MapFile{Data: make([]byte, 8*1024*1024)},
		"firmware/small-vars.fd": &fstest.MapFile{Data: make([]byte, 8*1024*1024)},
		"firmware/good.fd":       &fstest.MapFile{Data: make([]byte, 64*1024*1024)},
		"firmware/good-vars.fd":  &fstest.MapFile{Data: make([]byte, 64*1024*1024)},
	}

	statFn := func(path string) (fs.FileInfo, error) {
		return fs.Stat(mapFS, strings.TrimPrefix(path, "/"))
	}

	loader, vars := findAarch64UEFIFirmwareWithStat(candidates, statFn)
	if loader != "/firmware/good.fd" || vars != "/firmware/good-vars.fd" {
		t.Fatalf("unexpected firmware selection: loader=%q vars=%q", loader, vars)
	}
}

// Ensures XML switches key attributes based on the provided archSpec.
func TestGenerateVMXMLUsesArchSpec(t *testing.T) {
	t.Parallel()

	vmCfg := testVMConfig()
	diskPath := "/var/lib/libvirt/images/test.qcow2"
	cloudInitPath := "/var/lib/libvirt/images/test-cloud-init.iso"
	manager := &VMManager{config: &config.Config{}}

	armSpec := archSpec{
		libvirtArch: "aarch64",
		machine:     "virt",
		cpuMode:     "host-passthrough",
		emulator:    "/usr/bin/qemu-system-aarch64",
		uefiLoader:  "/usr/share/edk2/aarch64/QEMU_EFI-pflash.raw",
		enableACPI:  true,
	}
	armXML := manager.GenerateVMXML(vmCfg, diskPath, cloudInitPath, armSpec, "/var/lib/libvirt/qemu/nvram/test_VARS.fd")
	assertContains(t, armXML, "arch='aarch64'")
	assertContains(t, armXML, "machine='virt'")
	assertContains(t, armXML, "<emulator>/usr/bin/qemu-system-aarch64</emulator>")
	assertContains(t, armXML, "<nvram>/var/lib/libvirt/qemu/nvram/test_VARS.fd</nvram>")

	x86Spec := archSpec{
		libvirtArch: "x86_64",
		machine:     "q35",
		cpuMode:     "host-passthrough",
		emulator:    "/usr/libexec/qemu-kvm",
		enableIOMMU: true,
		enableAPIC:  true,
		enableACPI:  true,
	}
	x86XML := manager.GenerateVMXML(vmCfg, diskPath, cloudInitPath, x86Spec, "")
	assertContains(t, x86XML, "arch='x86_64'")
	assertContains(t, x86XML, "machine='q35'")
	assertContains(t, x86XML, "<emulator>/usr/libexec/qemu-kvm</emulator>")
	assertContains(t, x86XML, "<iommu model='intel'>")
}

func testVMConfig() config.VMConfig {
	return config.VMConfig{
		Name:     "test",
		Type:     "host",
		Memory:   2048,
		VCPUs:    2,
		DiskSize: 10,
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected XML to contain %q", needle)
	}
}
