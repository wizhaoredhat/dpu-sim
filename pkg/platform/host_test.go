package platform

import (
	"errors"
	"sync"
	"testing"
)

func resetHostDistroCacheForTest() {
	hostDistroOnce = sync.Once{}
	hostDistro = nil
	hostDistroErr = nil
	getHostDistroFn = func() (*Distro, error) {
		return NewLocalExecutor().GetDistro()
	}
}

func TestGetHostDistroCachesSuccess(t *testing.T) {
	resetHostDistroCacheForTest()
	t.Cleanup(resetHostDistroCacheForTest)

	calls := 0
	expected := &Distro{ID: "fedora", Architecture: X86_64, PackageManager: DNF}
	getHostDistroFn = func() (*Distro, error) {
		calls++
		return expected, nil
	}

	got1, err := GetHostDistro()
	if err != nil {
		t.Fatalf("GetHostDistro() unexpected error on first call: %v", err)
	}
	got2, err := GetHostDistro()
	if err != nil {
		t.Fatalf("GetHostDistro() unexpected error on second call: %v", err)
	}

	if calls != 1 {
		t.Fatalf("expected detector to be called once, got %d", calls)
	}
	if got1 != expected || got2 != expected {
		t.Fatalf("expected cached distro pointer to be reused")
	}
}

func TestGetHostDistroCachesError(t *testing.T) {
	resetHostDistroCacheForTest()
	t.Cleanup(resetHostDistroCacheForTest)

	calls := 0
	expectedErr := errors.New("detect failed")
	getHostDistroFn = func() (*Distro, error) {
		calls++
		return nil, expectedErr
	}

	_, err1 := GetHostDistro()
	_, err2 := GetHostDistro()

	if calls != 1 {
		t.Fatalf("expected detector to be called once, got %d", calls)
	}
	if !errors.Is(err1, expectedErr) {
		t.Fatalf("first error = %v, want %v", err1, expectedErr)
	}
	if !errors.Is(err2, expectedErr) {
		t.Fatalf("second error = %v, want %v", err2, expectedErr)
	}
}

func TestGetHostDistroReturnsFirstCachedValue(t *testing.T) {
	resetHostDistroCacheForTest()
	t.Cleanup(resetHostDistroCacheForTest)

	calls := 0
	first := &Distro{ID: "fedora", Architecture: X86_64, PackageManager: DNF}
	second := &Distro{ID: "ubuntu", Architecture: AARCH64, PackageManager: APT}
	current := first
	getHostDistroFn = func() (*Distro, error) {
		calls++
		return current, nil
	}

	got1, err := GetHostDistro()
	if err != nil {
		t.Fatalf("GetHostDistro() unexpected error on first call: %v", err)
	}
	current = second
	got2, err := GetHostDistro()
	if err != nil {
		t.Fatalf("GetHostDistro() unexpected error on second call: %v", err)
	}

	if calls != 1 {
		t.Fatalf("expected detector to be called once, got %d", calls)
	}
	if got1 != first || got2 != first {
		t.Fatalf("expected first distro to remain cached")
	}
}
