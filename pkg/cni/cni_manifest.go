package cni

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

const writableCNIBinDir = "/var/lib/cni/bin"

func (m *CNIManager) shouldUseWritableCNIBinDir() bool {
	if strings.TrimSpace(m.config.OperatingSystem.ImageRef) != "" {
		return true
	}

	for _, node := range m.config.BareMetal {
		if node.Bootc != nil && node.Bootc.Enabled {
			return true
		}
	}

	return false
}

func downloadManifest(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download manifest from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download manifest from %s: HTTP %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest body: %w", err)
	}

	return body, nil
}

func rewriteCNIBinPath(manifest []byte, hostPath string) []byte {
	content := string(manifest)
	hostMountPath := "/host" + hostPath
	content = strings.ReplaceAll(content, "/host/opt/cni/bin", hostMountPath)
	content = strings.ReplaceAll(content, "/opt/cni/bin", hostPath)
	return []byte(content)
}

func rewriteCNIHostPathOnly(manifest []byte, hostPath string) []byte {
	content := string(manifest)
	content = strings.ReplaceAll(content, "path: /opt/cni/bin", fmt.Sprintf("path: %s", hostPath))
	return []byte(content)
}
