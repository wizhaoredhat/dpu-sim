package vm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/log"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/retry"
)

const (
	// Default image directory
	DefaultImageDir = "/var/lib/libvirt/images"
)

var pullCloudImageFromOCI = pullOCICloudImage

// EnsureCloudImage makes sure the configured cloud image exists locally.
func EnsureCloudImage(osConfig config.OSConfig, destPath string) error {
	// Check if file already exists
	if _, err := os.Stat(destPath); err == nil {
		log.Info("✓ Image already exists at %s, skipping download", destPath)
		return nil
	}

	if osConfig.ImageRef != "" {
		return pullCloudImageFromOCI(osConfig.ImageRef, osConfig.ImageName, destPath)
	}
	if osConfig.ImageURL != "" {
		return DownloadCloudImage(osConfig.ImageURL, destPath)
	}
	return errors.New("operating_system image source is not configured")
}

// DownloadCloudImage downloads a cloud image if it doesn't exist
func DownloadCloudImage(url, destPath string) error {
	// Check if file already exists
	if _, err := os.Stat(destPath); err == nil {
		log.Info("✓ Image already exists at %s, skipping download", destPath)
		return nil
	}

	// Create destination directory if it doesn't exist
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", destDir, err)
	}

	log.Info("Downloading cloud image from %s to %s...", url, destPath)
	cmd := exec.Command("wget", "-O", destPath, url)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to download image: %w, output: %s", err, string(output))
	}

	log.Info("✓ Downloaded image to %s", destPath)
	return nil
}

func pullOCICloudImage(imageRef, imageName, destPath string) error {
	parsedRef, err := registry.ParseReference(imageRef)
	if err != nil {
		return fmt.Errorf("failed to parse OCI image reference %q: %w", imageRef, err)
	}
	if parsedRef.Reference == "" {
		return fmt.Errorf("OCI image reference %q must include a tag or digest", imageRef)
	}

	repo, err := remote.NewRepository(fmt.Sprintf("%s/%s", parsedRef.Registry, parsedRef.Repository))
	if err != nil {
		return fmt.Errorf("failed to create OCI repository client: %w", err)
	}

	ociClient := &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
	}
	if store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{}); err == nil {
		ociClient.Credential = credentials.Credential(store)
	} else {
		log.Debug("Unable to load docker credentials for OCI pull: %v", err)
	}
	repo.Client = ociClient

	tempDir, err := os.MkdirTemp("", "dpu-sim-oras-pull-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir for OCI pull: %w", err)
	}
	defer os.RemoveAll(tempDir)

	dstStore, err := file.New(tempDir)
	if err != nil {
		return fmt.Errorf("failed to create ORAS file store: %w", err)
	}
	defer dstStore.Close()
	dstStore.IgnoreNoName = true

	log.Info("Pulling OCI cloud image %s...", imageRef)
	ctx := context.Background()
	root, err := oras.Resolve(ctx, repo, parsedRef.Reference, oras.DefaultResolveOptions)
	if err != nil {
		return fmt.Errorf("failed to resolve OCI cloud image %s: %w", imageRef, err)
	}

	totalBytes := estimateOCIPullTotalBytes(ctx, repo, root)
	start := time.Now()
	var copiedBytes atomic.Int64

	countingDst := &progressStorage{
		Storage: dstStore,
		copied:  &copiedBytes,
	}

	copyOpts := oras.DefaultCopyGraphOptions
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				logOCIPullProgress(copiedBytes.Load(), totalBytes, start)
			}
		}
	}()

	// CopyGraph materializes artifact files without tagging the destination.
	if err := oras.CopyGraph(ctx, repo, countingDst, root, copyOpts); err != nil {
		close(done)
		return fmt.Errorf("failed to pull OCI cloud image %s: %w", imageRef, err)
	}
	close(done)
	logOCIPullProgress(copiedBytes.Load(), totalBytes, start)

	sourcePath, err := findPulledCloudImage(tempDir, imageName)
	if err != nil {
		return err
	}
	if err := copyFile(sourcePath, destPath); err != nil {
		return fmt.Errorf("failed to stage pulled cloud image %s: %w", sourcePath, err)
	}

	log.Info("✓ Pulled OCI cloud image %s to %s", imageRef, destPath)
	return nil
}

func estimateOCIPullTotalBytes(ctx context.Context, repo *remote.Repository, root ocispec.Descriptor) int64 {
	total := root.Size
	rc, err := repo.Fetch(ctx, root)
	if err != nil {
		return total
	}
	defer rc.Close()

	var manifest ocispec.Manifest
	if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
		return total
	}
	if manifest.Config.Size > 0 {
		total += manifest.Config.Size
	}
	for _, layer := range manifest.Layers {
		if layer.Size > 0 {
			total += layer.Size
		}
	}
	return total
}

func logOCIPullProgress(copiedBytes int64, totalBytes int64, start time.Time) {
	elapsed := time.Since(start)
	if elapsed <= 0 {
		elapsed = time.Second
	}
	speedBytesPerSec := float64(copiedBytes) / elapsed.Seconds()
	if totalBytes > 0 {
		pct := float64(copiedBytes) / float64(totalBytes) * 100
		if pct > 100 {
			pct = 100
		}
		log.Info("Pull progress: %s / %s (%.0f%%), %s/s, %s elapsed",
			formatBytes(copiedBytes), formatBytes(totalBytes), pct, formatBytes(int64(speedBytesPerSec)), elapsed.Round(time.Second))
		return
	}
	log.Info("Pull progress: %s downloaded, %s/s, %s elapsed",
		formatBytes(copiedBytes), formatBytes(int64(speedBytesPerSec)), elapsed.Round(time.Second))
}

func formatBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	value := float64(size)
	unit := "B"
	for _, next := range units {
		value /= 1024
		unit = next
		if value < 1024 {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}

type progressStorage struct {
	content.Storage
	copied *atomic.Int64
}

func (p *progressStorage) Push(ctx context.Context, expected ocispec.Descriptor, reader io.Reader) error {
	return p.Storage.Push(ctx, expected, &countingReader{
		reader: reader,
		copied: p.copied,
	})
}

type countingReader struct {
	reader io.Reader
	copied *atomic.Int64
}

func (c *countingReader) Read(buf []byte) (int, error) {
	n, err := c.reader.Read(buf)
	if n > 0 {
		c.copied.Add(int64(n))
	}
	return n, err
}

func findPulledCloudImage(rootDir, imageName string) (string, error) {
	files, err := listRegularFiles(rootDir)
	if err != nil {
		return "", err
	}

	if imageName != "" {
		expected := make([]string, 0, 1)
		for _, path := range files {
			if filepath.Base(path) == imageName {
				expected = append(expected, path)
			}
		}
		if len(expected) == 1 {
			return expected[0], nil
		}
		if len(expected) > 1 {
			return "", fmt.Errorf("multiple pulled files match image_name %q: %s", imageName, strings.Join(expected, ", "))
		}
	}

	qcow2Files := make([]string, 0, 1)
	for _, path := range files {
		if strings.EqualFold(filepath.Ext(path), ".qcow2") {
			qcow2Files = append(qcow2Files, path)
		}
	}
	if len(qcow2Files) == 1 {
		return qcow2Files[0], nil
	}
	if len(qcow2Files) == 0 {
		return "", fmt.Errorf("no %q or .qcow2 file found in pulled OCI artifact", imageName)
	}
	return "", fmt.Errorf("multiple .qcow2 files found in pulled OCI artifact: %s", strings.Join(qcow2Files, ", "))
}

func listRegularFiles(rootDir string) ([]string, error) {
	paths := make([]string, 0)
	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect pulled OCI artifact files: %w", err)
	}
	return paths, nil
}

func copyFile(sourcePath, destPath string) error {
	src, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return nil
}

// CreateVMDisk creates a disk image for a VM using qemu-img
func CreateVMDisk(vmName string, sizeGB int, baseImage string) (string, error) {
	diskPath := filepath.Join(DefaultImageDir, fmt.Sprintf("%s.qcow2", vmName))

	// Check if disk already exists
	if _, err := os.Stat(diskPath); err == nil {
		log.Info("✓ Disk for VM %s already exists at %s", vmName, diskPath)
		return diskPath, nil
	}

	// Create image directory if it doesn't exist
	if err := os.MkdirAll(DefaultImageDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create image directory: %w", err)
	}

	log.Debug("Creating disk for %s based on %s...", vmName, baseImage)
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2",
		"-F", "qcow2", "-b", baseImage, diskPath, fmt.Sprintf("%dG", sizeGB))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create disk: %w, output: %s", err, string(output))
	}

	log.Info("✓ Created disk for %s: %s", vmName, diskPath)
	return diskPath, nil
}

// CreateCloudInitISO creates a cloud-init ISO for VM initialization
func CreateCloudInitISO(vmName string, sshConfig config.SSHConfig, vmConfig config.VMConfig) (string, error) {
	vmName = vmConfig.Name
	isoPath := filepath.Join(DefaultImageDir, fmt.Sprintf("%s-cloud-init.iso", vmName))

	if _, err := os.Stat(isoPath); err == nil {
		log.Info("✓ Cloud-init ISO for %s already exists at %s", vmName, isoPath)
		return isoPath, nil
	}

	// Create temporary directory for cloud-init files
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("cloud-init-%s-", vmName))
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Create meta-data file
	metaDataPath := filepath.Join(tempDir, "meta-data")
	metaData := generateMetaData(vmName)
	if err := os.WriteFile(metaDataPath, []byte(metaData), 0644); err != nil {
		return "", fmt.Errorf("failed to write meta-data: %w", err)
	}

	// Read SSH public key
	pubKeyPath := sshConfig.KeyPath + ".pub"
	pubKeyData, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to read SSH public key: %w", err)
	}

	// Create user-data file
	userData := generateUserData(string(pubKeyData), sshConfig.User, sshConfig.Password)
	userDataPath := filepath.Join(tempDir, "user-data")
	if err := os.WriteFile(userDataPath, []byte(userData), 0644); err != nil {
		return "", fmt.Errorf("failed to write user-data: %w", err)
	}

	if _, err := exec.LookPath("genisoimage"); err != nil {
		return "", fmt.Errorf("genisoimage not found in PATH err:%s", err)
	}

	cmd := exec.Command("genisoimage", "-output", isoPath, "-volid", "cidata",
		"-joliet", "-rock", userDataPath, metaDataPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create cloud-init ISO: %w, output: %s", err, string(output))
	}

	log.Info("✓ Created cloud-init ISO: %s", isoPath)
	return isoPath, nil
}

// generateMetaData generates cloud-init meta-data content with the name of the VM.
func generateMetaData(vmName string) string {
	var sb strings.Builder

	sb.WriteString("instance-id: " + vmName + "\n")
	sb.WriteString("local-hostname: " + vmName + "\n")
	return sb.String()
}

// generateUserData generates cloud-init user-data content that sets ssh keys, passwords, updates packages, and disables
// zram. ZRAM enables swap, which is not desirable for k8s, hense we disable it partially here.
func generateUserData(sshPubKey, username, password string) string {
	var sb strings.Builder

	sb.WriteString("#cloud-config\n")

	sb.WriteString("users:\n")
	sb.WriteString("  - name: " + username + "\n")
	sb.WriteString("    sudo: ALL=(ALL) NOPASSWD:ALL\n")
	sb.WriteString("    groups: wheel\n")
	sb.WriteString("    shell: /bin/bash\n")
	sb.WriteString("    ssh_authorized_keys:\n")
	sb.WriteString("      - " + strings.TrimSpace(sshPubKey) + "\n")

	sb.WriteString("\n# Set password for console access (password: " + password + ")\n")
	sb.WriteString("chpasswd:\n")
	sb.WriteString("  list: |\n")
	sb.WriteString("    " + username + ":" + password + "\n")
	sb.WriteString("  expire: false\n")

	sb.WriteString("\n# Enable password authentication for emergency access\n")
	sb.WriteString("ssh_pwauth: true\n")

	sb.WriteString("\n# Update packages\n")
	sb.WriteString("package_update: true\n")
	sb.WriteString("package_upgrade: false\n")

	sb.WriteString("\n# Additional packages\n")
	sb.WriteString("packages:\n")
	sb.WriteString("  - curl\n")
	sb.WriteString("  - wget\n")

	sb.WriteString("\n# ZRAM configuration\n")
	sb.WriteString("write_files:\n")
	sb.WriteString("  - path: /etc/systemd/zram-generator.conf\n")
	sb.WriteString("    content: \"\"\n")
	sb.WriteString("    permissions: \"0644\"\n")

	sb.WriteString("\n# Start services\n")
	sb.WriteString("runcmd:\n")
	sb.WriteString("  - systemctl enable sshd\n")
	sb.WriteString("  - systemctl start sshd\n")
	sb.WriteString("  - systemctl daemon-reload\n")
	sb.WriteString("  - systemctl restart zram-generator.service\n")

	return sb.String()
}

// DeleteVMDisk deletes a VM disk image
func DeleteVMDisk(vmName string) error {
	diskPath := filepath.Join(DefaultImageDir, fmt.Sprintf("%s.qcow2", vmName))

	if _, err := os.Stat(diskPath); os.IsNotExist(err) {
		log.Info("✓ Disk for %s does not exist, skipping deletion", vmName)
		return nil
	}

	if err := os.Remove(diskPath); err != nil {
		return fmt.Errorf("failed to delete disk %s: %w", diskPath, err)
	}

	log.Info("✓ Deleted disk: %s", diskPath)
	return nil
}

// DeleteCloudInitISO deletes a cloud-init ISO
func DeleteCloudInitISO(vmName string) error {
	isoPath := filepath.Join(DefaultImageDir, fmt.Sprintf("%s-cloud-init.iso", vmName))

	if _, err := os.Stat(isoPath); os.IsNotExist(err) {
		log.Info("✓ Cloud-init ISO for %s does not exist, skipping deletion", vmName)
		return nil
	}

	if err := os.Remove(isoPath); err != nil {
		return fmt.Errorf("failed to delete cloud-init ISO %s: %w", isoPath, err)
	}

	log.Info("✓ Deleted cloud-init ISO: %s", isoPath)
	return nil
}

// DeleteUEFINvram deletes any per-VM UEFI NVRAM files.
func DeleteUEFINvram(vmName string) error {
	pattern := filepath.Join("/var/lib/libvirt/qemu/nvram", fmt.Sprintf("%s_VARS*", vmName))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to glob UEFI NVRAM files for %s: %w", vmName, err)
	}
	if len(matches) == 0 {
		fmt.Printf("✓ UEFI NVRAM for %s does not exist, skipping deletion\n", vmName)
		return nil
	}

	for _, path := range matches {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete UEFI NVRAM %s: %w", path, err)
		}
		fmt.Printf("✓ Deleted UEFI NVRAM: %s\n", path)
	}

	return nil
}

// GetImagePath returns the path where an OS image should be stored
func GetImagePath(osConfig config.OSConfig) string {
	filename := filepath.Base(osConfig.ImageName)
	return filepath.Join(DefaultImageDir, filename)
}
