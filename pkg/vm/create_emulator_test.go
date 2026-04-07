package vm

import (
	"fmt"
	"testing"
)

// TestResolveFirstAvailableEmulatorPrefersExistingAbsolutePath verifies an
// existing absolute-path candidate is selected before PATH lookups.
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

// TestResolveFirstAvailableEmulatorFallsBackToLookPath verifies resolver falls
// back to lookPath when no absolute candidate exists.
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

// TestResolveFirstAvailableEmulatorReturnsFalseWhenNoneFound verifies resolver
// reports failure when no candidates are present on disk or in PATH.
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
