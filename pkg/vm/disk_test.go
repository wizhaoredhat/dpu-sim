package vm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wizhao/dpu-sim/pkg/config"
)

func TestEnsureCloudImageUsesOCIRef(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "Fedora-x86_64.qcow2")

	called := false
	origPull := pullCloudImageFromOCI
	pullCloudImageFromOCI = func(imageRef, imageName, destPath string) error {
		called = true
		if imageRef != "ghcr.io/example/fedora:43" {
			t.Fatalf("unexpected imageRef: %s", imageRef)
		}
		if imageName != "Fedora-x86_64.qcow2" {
			t.Fatalf("unexpected imageName: %s", imageName)
		}
		if destPath != dest {
			t.Fatalf("unexpected destPath: %s", destPath)
		}
		return nil
	}
	t.Cleanup(func() {
		pullCloudImageFromOCI = origPull
	})

	err := EnsureCloudImage(config.OSConfig{
		ImageRef:  "ghcr.io/example/fedora:43",
		ImageName: "Fedora-x86_64.qcow2",
	}, dest)
	if err != nil {
		t.Fatalf("EnsureCloudImage returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected OCI pull helper to be called")
	}
}

func TestEnsureCloudImageSkipsExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "Fedora-x86_64.qcow2")
	if err := os.WriteFile(dest, []byte("existing"), 0o644); err != nil {
		t.Fatalf("failed to prepare existing image: %v", err)
	}

	called := false
	origPull := pullCloudImageFromOCI
	pullCloudImageFromOCI = func(_, _, _ string) error {
		called = true
		return nil
	}
	t.Cleanup(func() {
		pullCloudImageFromOCI = origPull
	})

	err := EnsureCloudImage(config.OSConfig{
		ImageRef:  "ghcr.io/example/fedora:43",
		ImageName: "Fedora-x86_64.qcow2",
	}, dest)
	if err != nil {
		t.Fatalf("EnsureCloudImage returned error: %v", err)
	}
	if called {
		t.Fatalf("expected OCI pull helper not to be called when file already exists")
	}
}

func TestFindPulledCloudImage(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	want := filepath.Join(tmpDir, "nested", "Fedora-x86_64.qcow2")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}
	if err := os.WriteFile(want, []byte("qcow2"), 0o644); err != nil {
		t.Fatalf("failed to write image: %v", err)
	}

	got, err := findPulledCloudImage(tmpDir, "Fedora-x86_64.qcow2")
	if err != nil {
		t.Fatalf("findPulledCloudImage returned error: %v", err)
	}
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}
