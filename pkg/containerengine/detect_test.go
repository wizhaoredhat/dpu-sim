package containerengine

import "testing"

func TestParseName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Name
		wantErr bool
	}{
		{name: "empty maps to auto", input: "", want: EngineAuto},
		{name: "auto", input: "auto", want: EngineAuto},
		{name: "docker", input: "docker", want: EngineDocker},
		{name: "podman", input: "podman", want: EnginePodman},
		{name: "case and spacing", input: "  DoCkEr  ", want: EngineDocker},
		{name: "invalid", input: "containerd", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseName(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDetectName(t *testing.T) {
	tests := []struct {
		name    string
		in      DetectionInput
		want    Name
		wantErr bool
	}{
		{name: "preferred wins", in: DetectionInput{Preferred: EnginePodman}, want: EnginePodman},
		{name: "preferred invalid errors", in: DetectionInput{Preferred: Name("bad")}, wantErr: true},
		{name: "empty input falls back auto", in: DetectionInput{}, want: EngineAuto},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DetectName(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
