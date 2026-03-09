package vm

import (
	"crypto/sha256"
	"fmt"
)

// OUI 52:54:00 is commonly used for QEMU/virtio; we use it so MACs are locally administered and deterministic.
const macOUI = "52:54:00"

// GenerateMACForNetwork returns a deterministic MAC for a VM's network interface (mgmt, k8s, etc.).
// Hash is over vmName+networkType so the same VM+type always gets the same MAC.
func GenerateMACForNetwork(vmName, networkType string) string {
	h := sha256.Sum256([]byte(vmName + "\x00" + networkType))
	return fmt.Sprintf("%s:%02x:%02x:%02x", macOUI, h[0], h[1], h[2])
}

// GenerateMACForHostToDpu returns a deterministic MAC for a host-to-DPU data interface.
// Hash is over vmName+role ("host" or "dpu"); the index is the last octet so each pair has a unique MAC.
func GenerateMACForHostToDpu(vmName, role string, index int) string {
	h := sha256.Sum256([]byte(vmName + "\x00" + role))
	return fmt.Sprintf("%s:%02x:%02x:%02x", macOUI, h[0], h[1], index&0xff)
}
