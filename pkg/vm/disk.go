package vm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/log"
)

const (
	// Default image directory
	DefaultImageDir = "/var/lib/libvirt/images"
)

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

// GetImagePath returns the path where an OS image should be stored
func GetImagePath(osConfig config.OSConfig) string {
	filename := filepath.Base(osConfig.ImageName)
	return filepath.Join(DefaultImageDir, filename)
}
