package control

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPauseAndTrigger(t *testing.T) {
	dir := t.TempDir()
	if err := SetPaused(dir, true); err != nil {
		t.Fatal(err)
	}
	if !IsPaused(dir) {
		t.Fatal("expected paused")
	}
	if err := SetPaused(dir, false); err != nil {
		t.Fatal(err)
	}
	if IsPaused(dir) {
		t.Fatal("expected not paused")
	}

	if err := RequestRunNow(dir); err != nil {
		t.Fatal(err)
	}
	if !ConsumeTrigger(dir) {
		t.Fatal("expected trigger consumed")
	}
	if _, err := os.Stat(filepath.Join(dir, ".runner", "trigger")); !os.IsNotExist(err) {
		t.Fatal("trigger file should be removed")
	}
}
