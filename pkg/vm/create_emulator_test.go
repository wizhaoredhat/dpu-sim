package vm

import (
	"fmt"
	"testing"
)

func TestResolveFirstAvailableEmulatorPrefersExistingAbsolutePath(t *testing.T) {
	t.Parallel()

	paths := map[string]bool{
		"/usr/libexec/qemu-kvm":       true,
		"/usr/bin/qemu-system-x86_64": true,
	}

	got, ok := resolveFirstAvailableEmulator(
		[]string{"/usr/libexec/qemu-kvm", "qemu-system-x86_64"},
		func(path string) bool { return paths[path] },
		func(bin string) (string, error) { return "", fmt.Errorf("should not be called") },
	)

	if !ok {
		t.Fatalf("expected emulator resolution to succeed")
	}
	if got != "/usr/libexec/qemu-kvm" {
		t.Fatalf("unexpected emulator path: %q", got)
	}
}

func TestResolveFirstAvailableEmulatorFallsBackToLookPath(t *testing.T) {
	t.Parallel()

	got, ok := resolveFirstAvailableEmulator(
		[]string{"/usr/libexec/qemu-kvm", "qemu-system-x86_64"},
		func(path string) bool { return false },
		func(bin string) (string, error) {
			if bin == "qemu-system-x86_64" {
				return "/usr/bin/qemu-system-x86_64", nil
			}
			return "", fmt.Errorf("not found")
		},
	)

	if !ok {
		t.Fatalf("expected emulator resolution to succeed")
	}
	if got != "/usr/bin/qemu-system-x86_64" {
		t.Fatalf("unexpected emulator path: %q", got)
	}
}

func TestResolveFirstAvailableEmulatorReturnsFalseWhenNoneFound(t *testing.T) {
	t.Parallel()

	_, ok := resolveFirstAvailableEmulator(
		[]string{"/usr/libexec/qemu-kvm", "qemu-system-x86_64"},
		func(path string) bool { return false },
		func(bin string) (string, error) { return "", fmt.Errorf("not found") },
	)

	if ok {
		t.Fatalf("expected emulator resolution to fail")
	}
}
