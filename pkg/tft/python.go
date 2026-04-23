package tft

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

const tftVenvDir = ".tft-venv"

const (
	tftMinPythonMajor = 3
	tftMinPythonMinor = 11
)

// ErrPythonTooOld means the interpreter is below the minimum for kubernetes-traffic-flow-tests.
var ErrPythonTooOld = errors.New("python < 3.11")

// CheckTFTPythonVersion returns nil if Python executable runs Python >= tftMinPythonMajor/tftMinPythonMinor.
func CheckTFTPythonVersion(python string) error {
	python = strings.TrimSpace(python)
	if python == "" {
		return fmt.Errorf("empty Python executable path")
	}

	probe := fmt.Sprintf(
		"import sys; raise SystemExit(0 if sys.version_info[:2] >= (%d, %d) else 1)",
		tftMinPythonMajor, tftMinPythonMinor,
	)

	cmd := exec.Command(python, "-c", probe)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return fmt.Errorf("%s: TFT requires Python >= %d.%d: %w", python, tftMinPythonMajor, tftMinPythonMinor, ErrPythonTooOld)
		}
		return fmt.Errorf("%s: %w\n%s", python, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DiscoverHostPython picks a host Python >= 3.11 to create the TFT venv.
// Order: PYTHON (must satisfy version check), then LookPath for python3.13, python3.12, python3.11, python3.
func DiscoverHostPython() (string, error) {
	if p := strings.TrimSpace(os.Getenv("PYTHON")); p != "" {
		if err := CheckTFTPythonVersion(p); err != nil {
			return "", fmt.Errorf("PYTHON is set but not usable for TFT: %w", err)
		}
		return p, nil
	}

	names := []string{"python3.13", "python3.12", "python3.11", "python3"}
	for _, name := range names {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := CheckTFTPythonVersion(path); err != nil {
			if errors.Is(err, ErrPythonTooOld) {
				continue
			}
			return "", err
		}
		return path, nil
	}

	return "", fmt.Errorf("no Python >= %d.%d found in PATH (tried %v); install a newer version or set PYTHON", tftMinPythonMajor, tftMinPythonMinor, names)
}

// VenvPython returns the interpreter inside the TFT repo venv if it exists.
func VenvPython(tftRepo string) (string, bool) {
	tftRepo = strings.TrimSpace(tftRepo)
	if tftRepo == "" {
		return "", false
	}
	candidates := venvPythonCandidates(tftRepo)
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

func venvPythonCandidates(tftRepo string) []string {
	base := filepath.Join(tftRepo, tftVenvDir, "bin")
	return []string{
		filepath.Join(base, "python3"),
		filepath.Join(base, "python"),
	}
}

// PythonForTFTRun resolves the interpreter for tft.py (always Python >= tftMin).
// If override is non-empty, it is used after a version check. Otherwise prefer the repo venv, then DiscoverHostPython().
func PythonForTFTRun(tftRepo, override string) (string, error) {
	if p := strings.TrimSpace(override); p != "" {
		if err := CheckTFTPythonVersion(p); err != nil {
			return "", err
		}
		return p, nil
	}
	if p, ok := VenvPython(tftRepo); ok {
		if err := CheckTFTPythonVersion(p); err != nil {
			return "", fmt.Errorf("TFT venv under %q uses Python < %d.%d; remove directory %q and run \"dpu-sim tft venv --python <python3.11+>\": %w",
				tftRepo, tftMinPythonMajor, tftMinPythonMinor, tftVenvDir, err)
		}
		return p, nil
	}
	return DiscoverHostPython()
}

// EnsureVenv creates tftRepo/.tft-venv when missing (using hostPython), then pip installs requirements.txt.
// If a venv already exists, python -m venv is skipped; pip steps still run.
func EnsureVenv(cmdExec platform.CommandExecutor, tftRepo, hostPython string) error {
	tftRepo = strings.TrimSpace(tftRepo)
	if tftRepo == "" {
		return fmt.Errorf("tft repo path is empty")
	}

	req := filepath.Join(tftRepo, "requirements.txt")
	if _, err := os.Stat(req); err != nil {
		return fmt.Errorf("requirements.txt not found in %s: %w", tftRepo, err)
	}

	venvPath := filepath.Join(tftRepo, tftVenvDir)
	py, ok := VenvPython(tftRepo)
	if !ok {
		hostPy := strings.TrimSpace(hostPython)
		var err error
		if hostPy == "" {
			hostPy, err = DiscoverHostPython()
			if err != nil {
				return err
			}
		} else if err := CheckTFTPythonVersion(hostPy); err != nil {
			return fmt.Errorf("host python for venv: %w", err)
		}

		log.Info("Creating TFT venv at %s using %s", venvPath, hostPy)
		if err := cmdExec.RunCmdInDir(log.LevelInfo, tftRepo, hostPy, "-m", "venv", venvPath); err != nil {
			return fmt.Errorf("python -m venv: %w", err)
		}
		py, ok = VenvPython(tftRepo)
		if !ok {
			return fmt.Errorf("venv python not found under %s after venv create", venvPath)
		}
	} else {
		log.Info("TFT venv already exists at %s, skipping python -m venv", venvPath)
	}
	if err := CheckTFTPythonVersion(py); err != nil {
		return fmt.Errorf("TFT venv interpreter %s: %w", py, err)
	}
	for _, step := range [][]string{
		{"-m", "pip", "install", "-U", "pip"},
		{"-m", "pip", "install", "-r", "requirements.txt"},
	} {
		args := append([]string{py}, step...)
		if err := cmdExec.RunCmdInDir(log.LevelInfo, tftRepo, args[0], args[1:]...); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(args, " "), err)
		}
	}
	log.Info("TFT venv ready: %s", py)
	return nil
}
