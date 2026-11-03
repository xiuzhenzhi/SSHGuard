package agent

import (
	"os"
	"runtime"
	"runtime/debug"
	"testing"
)

func TestMemoryTargetHonoursOperator(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(previousProcs)
	previous := debug.SetMemoryLimit(-1)
	defer debug.SetMemoryLimit(previous)
	t.Setenv("GOMEMLIMIT", "64MiB")
	t.Setenv("GOMAXPROCS", "4")
	runtime.GOMAXPROCS(4)
	debug.SetMemoryLimit(64 << 20)
	configureResources()
	if debug.SetMemoryLimit(-1) != 64<<20 {
		t.Fatal("operator memory target overwritten")
	}
	if runtime.GOMAXPROCS(0) != 4 {
		t.Fatal("operator scheduler setting overwritten")
	}
	os.Unsetenv("GOMEMLIMIT")
	os.Unsetenv("GOMAXPROCS")
	configureResources()
	if debug.SetMemoryLimit(-1) != defaultMemoryLimit {
		t.Fatal("default target not applied")
	}
	if runtime.GOMAXPROCS(0) != 2 {
		t.Fatal("default scheduler cap not applied")
	}
}
