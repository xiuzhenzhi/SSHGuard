package agent

import (
	"os"
	"runtime"
	"runtime/debug"
)

const defaultMemoryLimit = 32 << 20

func configureResources() {
	// Collection and reporting are predominantly I/O, with only two active
	// pipelines. Do not size scheduler/GC workers to a large server's core count.
	if _, set := os.LookupEnv("GOMAXPROCS"); !set && runtime.GOMAXPROCS(0) > 2 {
		runtime.GOMAXPROCS(2)
	}
	// Bound GC-managed memory during bursts, without forcing frequent full GCs.
	// Honour operator settings. This is a soft Go-runtime target, not an RSS cap:
	// executable pages, OS event APIs and the durable queue's mmap are excluded.
	if _, set := os.LookupEnv("GOMEMLIMIT"); !set {
		debug.SetMemoryLimit(defaultMemoryLimit)
	}
}
