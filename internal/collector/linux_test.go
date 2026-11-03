//go:build linux

package collector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCPUPercentFirstSampleIsZero(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "stat"), []byte("cpu 100 0 50 850 0 0 0 0 0 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := &Linux{ProcRoot: root}
	got, err := c.cpuPercent()
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf("first CPU sample = %v, want 0", got)
	}
	if err := os.WriteFile(filepath.Join(root, "stat"), []byte("cpu 150 0 75 975 0 0 0 0 0 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = c.cpuPercent()
	if err != nil {
		t.Fatal(err)
	}
	if got < 0 || got > 100 {
		t.Fatalf("CPU percentage out of bounds: %v", got)
	}
}
