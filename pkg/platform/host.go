package platform

import "sync"

var (
	hostDistroOnce  sync.Once
	hostDistro      *Distro
	hostDistroErr   error
	getHostDistroFn = func() (*Distro, error) {
		return NewLocalExecutor().GetDistro()
	}
)

// GetHostDistro detects and caches the local host distro once per process.
func GetHostDistro() (*Distro, error) {
	hostDistroOnce.Do(func() {
		hostDistro, hostDistroErr = getHostDistroFn()
	})
	return hostDistro, hostDistroErr
}
