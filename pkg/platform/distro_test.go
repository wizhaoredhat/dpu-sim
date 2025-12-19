package platform

import "testing"

func TestDistro_IsFedoraLike(t *testing.T) {
	tests := []struct {
		name   string
		distro Distro
		want   bool
	}{
		{"fedora", Distro{ID: "fedora"}, true},
		{"rhel", Distro{ID: "rhel"}, true},
		{"centos", Distro{ID: "centos"}, true},
		{"rocky", Distro{ID: "rocky"}, true},
		{"almalinux", Distro{ID: "almalinux"}, true},
		{"fedora-like via ID_LIKE", Distro{ID: "custom", IDLike: "fedora"}, true},
		{"rhel-like via ID_LIKE", Distro{ID: "custom", IDLike: "rhel centos"}, true},
		{"ubuntu", Distro{ID: "ubuntu"}, false},
		{"debian", Distro{ID: "debian"}, false},
		{"alpine", Distro{ID: "alpine"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.distro.IsFedoraLike(); got != tt.want {
				t.Errorf("IsFedoraLike() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDistro_IsDebianLike(t *testing.T) {
	tests := []struct {
		name   string
		distro Distro
		want   bool
	}{
		{"debian", Distro{ID: "debian"}, true},
		{"ubuntu", Distro{ID: "ubuntu"}, true},
		{"linuxmint", Distro{ID: "linuxmint"}, true},
		{"pop", Distro{ID: "pop"}, true},
		{"debian-like via ID_LIKE", Distro{ID: "custom", IDLike: "debian"}, true},
		{"ubuntu-like via ID_LIKE", Distro{ID: "custom", IDLike: "ubuntu"}, true},
		{"fedora", Distro{ID: "fedora"}, false},
		{"rhel", Distro{ID: "rhel"}, false},
		{"alpine", Distro{ID: "alpine"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.distro.IsDebianLike(); got != tt.want {
				t.Errorf("IsDebianLike() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectPackageManager(t *testing.T) {
	tests := []struct {
		name   string
		distro Distro
		want   string
	}{
		{"fedora uses dnf", Distro{ID: "fedora"}, DNF},
		{"rhel uses dnf", Distro{ID: "rhel"}, DNF},
		{"centos uses dnf", Distro{ID: "centos"}, DNF},
		{"ubuntu uses apt", Distro{ID: "ubuntu"}, APT},
		{"debian uses apt", Distro{ID: "debian"}, APT},
		{"alpine uses apk", Distro{ID: "alpine"}, APK},
		{"unknown distro", Distro{ID: "unknown"}, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectPackageManager(&tt.distro); got != tt.want {
				t.Errorf("DetectPackageManager() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    Distro
	}{
		{
			name: "fedora 43",
			content: `NAME="Fedora Linux"
VERSION="43 (Workstation Edition)"
ID=fedora
VERSION_ID=43
PLATFORM_ID="platform:f43"
PRETTY_NAME="Fedora Linux 43 (Workstation Edition)"`,
			want: Distro{
				ID:             "fedora",
				VersionID:      "43",
				PackageManager: DNF,
			},
		},
		{
			name: "ubuntu 22.04",
			content: `NAME="Ubuntu"
VERSION="22.04.3 LTS (Jammy Jellyfish)"
ID=ubuntu
ID_LIKE=debian
VERSION_ID="22.04"
PRETTY_NAME="Ubuntu 22.04.3 LTS"`,
			want: Distro{
				ID:             "ubuntu",
				VersionID:      "22.04",
				IDLike:         "debian",
				PackageManager: APT,
			},
		},
		{
			name: "rhel 9",
			content: `NAME="Red Hat Enterprise Linux"
VERSION="9.2 (Plow)"
ID="rhel"
ID_LIKE="fedora"
VERSION_ID="9.2"`,
			want: Distro{
				ID:             "rhel",
				VersionID:      "9.2",
				IDLike:         "fedora",
				PackageManager: DNF,
			},
		},
		{
			name: "alpine",
			content: `NAME="Alpine Linux"
ID=alpine
VERSION_ID=3.18.4`,
			want: Distro{
				ID:             "alpine",
				VersionID:      "3.18.4",
				PackageManager: APK,
			},
		},
		{
			name:    "empty content",
			content: "",
			want: Distro{
				PackageManager: "unknown",
			},
		},
		{
			name: "with comments",
			content: `# This is a comment
ID=fedora
# Another comment
VERSION_ID=43`,
			want: Distro{
				ID:             "fedora",
				VersionID:      "43",
				PackageManager: DNF,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.content)
			if got.ID != tt.want.ID {
				t.Errorf("Parse() ID = %v, want %v", got.ID, tt.want.ID)
			}
			if got.VersionID != tt.want.VersionID {
				t.Errorf("Parse() VersionID = %v, want %v", got.VersionID, tt.want.VersionID)
			}
			if got.IDLike != tt.want.IDLike {
				t.Errorf("Parse() IDLike = %v, want %v", got.IDLike, tt.want.IDLike)
			}
			if got.PackageManager != tt.want.PackageManager {
				t.Errorf("Parse() PackageManager = %v, want %v", got.PackageManager, tt.want.PackageManager)
			}
		})
	}
}
