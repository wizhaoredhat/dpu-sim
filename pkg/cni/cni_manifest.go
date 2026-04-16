package cni

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const writableCNIBinDir = "/var/lib/cni/bin"

func (m *CNIManager) shouldUseWritableCNIBinDir() bool {
	// Kind node images stage plugins under /opt/cni/bin (see linux.InstallKindCNIPlugins).
	if m.config.IsKindMode() {
		return false
	}

	// VM and bare-metal installs run linux.EnsureCRIOCNIPluginPaths: it copies
	// plugins into /var/lib/cni/bin and (when possible) symlinks the same names
	// under /opt/cni/bin -> /usr/libexec/cni/... Multus thick defaults to
	// binDir /opt/cni/bin; delegate plugins run from the Multus pod mount namespace.
	// Symlinks under /opt/cni/bin then break: the target path is not mounted in
	// the pod, so libcni reports e.g. "failed to find plugin portmap" even though
	// `ls /opt/cni/bin/portmap` on the host succeeds. /var/lib/cni/bin holds real
	// file copies, so point Multus/Flannel/Whereabouts there.
	if m.config.IsVMMode() {
		return true
	}

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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create manifest download request for %s: %w", url, err)
	}

	resp, err := http.DefaultClient.Do(req)
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
