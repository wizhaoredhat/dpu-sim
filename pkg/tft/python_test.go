package tft

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckTFTPythonVersion_realPython3(t *testing.T) {
	t.Parallel()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3 on PATH")
	}
	if err := CheckTFTPythonVersion(py); err != nil {
		t.Skip("python3 is not >= 3.11:", err)
	}
}

func TestPythonForTFTRun_Override(t *testing.T) {
	t.Parallel()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3 on PATH")
	}
	if err := CheckTFTPythonVersion(py); err != nil {
		t.Skip("python3 is not >= 3.11:", err)
	}
	got, err := PythonForTFTRun("/tmp/repo", py)
	require.NoError(t, err)
	require.Equal(t, py, got)
}

func TestPythonForTFTRun_PrefersVenv(t *testing.T) {
	t.Parallel()
	real, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3 on PATH")
	}
	if err := CheckTFTPythonVersion(real); err != nil {
		t.Skip("python3 is not >= 3.11:", err)
	}
	repo := t.TempDir()
	bin := filepath.Join(repo, tftVenvDir, "bin")
	require.NoError(t, os.MkdirAll(bin, 0o755))
	want := filepath.Join(bin, "python3")
	require.NoError(t, os.Symlink(real, want))

	got, err := PythonForTFTRun(repo, "")
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestVenvPython_Missing(t *testing.T) {
	t.Parallel()
	_, ok := VenvPython(t.TempDir())
	require.False(t, ok)
}
