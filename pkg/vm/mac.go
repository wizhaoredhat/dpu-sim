package vm

import (
	"crypto/sha256"
	"fmt"

	"github.com/wizhao/dpu-sim/pkg/network"
)

// GenerateMACForNetwork returns a deterministic MAC for a VM's network interface (mgmt, k8s, etc.).
// Hash is over vmName+networkType so the same VM+type always gets the same MAC.
func GenerateMACForNetwork(vmName, networkType string) string {
	h := sha256.Sum256([]byte(vmName + "\x00" + networkType))
	return fmt.Sprintf("%s:%02x:%02x:%02x", network.MacOUI, h[0], h[1], h[2])
}
