package containerengine

import (
	"fmt"
	"strings"
)

// DetectionInput captures explicit selection preferences.
type DetectionInput struct {
	Preferred Name
}

// DetectName returns the selected runtime name from explicit preference.
// Empty (or auto) means auto-detection should be used by callers.
func DetectName(in DetectionInput) (Name, error) {
	if in.Preferred != "" && in.Preferred != EngineAuto {
		return ParseName(string(in.Preferred))
	}
	return EngineAuto, nil
}

// ParseName validates and normalizes a runtime name.
func ParseName(raw string) (Name, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", "auto":
		return EngineAuto, nil
	case "docker":
		return EngineDocker, nil
	case "podman":
		return EnginePodman, nil
	default:
		return "", fmt.Errorf("unsupported container engine %q", raw)
	}
}
