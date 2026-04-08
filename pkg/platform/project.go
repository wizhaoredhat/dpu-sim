package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	projectRootOnce sync.Once
	projectRoot     string
	projectRootErr  error
)

// ProjectRoot returns the root directory of the dpu-sim project.
// It walks up from the calling source file until it finds a directory
// containing go.mod, so it works regardless of which package calls it.
// The result is cached for the lifetime of the process.
func GetProjectRoot() (string, error) {
	projectRootOnce.Do(func() {
		_, filename, _, ok := runtime.Caller(0)
		if !ok {
			projectRootErr = fmt.Errorf("failed to get current file path")
			return
		}

		dir := filepath.Dir(filename)
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				projectRoot = dir
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
		projectRootErr = fmt.Errorf("could not find project root (go.mod) from %s", filename)
	})
	return projectRoot, projectRootErr
}
