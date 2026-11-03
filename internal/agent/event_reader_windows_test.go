package agent

import (
	"context"
	"encoding/xml"
	"fmt"
	"os/exec"
	"reflect"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestNativeEventsMatchWevtutilData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	latest, err := readWindowsEvents(ctx, "qe", "Application", "/rd:true", "/c:1")
	if err != nil || len(latest) == 0 {
		t.Fatal("native latest", err)
	}
	query := fmt.Sprintf("/q:*[System[EventRecordID <= %d]]", latest[0].System.RecordID)
	args := []string{"qe", "Application", "/f:RenderedXml", "/e:Events", "/uni:true", "/rd:true", "/c:3", query}
	raw, err := exec.CommandContext(ctx, "wevtutil.exe", args...).Output()
	if err != nil {
		t.Fatal(err)
	}
	var previous struct {
		Events []windowsEvent `xml:"Event"`
	}
	if err = xml.Unmarshal(decodeWindowsXML(raw), &previous); err != nil {
		t.Fatal(err)
	}
	current, err := readWindowsEvents(ctx, args...)
	if err != nil {
		t.Fatal(err)
	}
	if len(previous.Events) != len(current) {
		t.Fatal("native event count differs")
	}
	for i := range current {
		// The native reader deliberately skips localized RenderingInfo and keeps
		// original EventData, including Unicode and named Security fields.
		if !reflect.DeepEqual(previous.Events[i].System, current[i].System) || !reflect.DeepEqual(previous.Events[i].Data, current[i].Data) {
			t.Fatal("native event metadata/data differ", i)
		}
	}
	cancel()
	if _, err = readWindowsEvents(ctx, args...); err != context.Canceled {
		t.Fatal("cancellation ignored", err)
	}
}

func TestNativeEventHandlesAreReleased(t *testing.T) {
	// GC workers on a many-core host create additional process handles even
	// when queries release all handles. Use the Agent's worker budget here.
	previousProcs := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previousProcs)
	// Keep the RPC thread cache and Go thread count stable while measuring native
	// query/event handles. Each production query also locks its owning thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	countProc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessHandleCount")
	count := func() uint32 {
		t.Helper()
		var n uint32
		ok, _, err := countProc.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&n)))
		if ok == 0 {
			t.Fatal(err)
		}
		return n
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	read := func() {
		t.Helper()
		if _, err := readWindowsEvents(ctx, "qe", "Application", "/rd:true", "/c:1"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 10; i++ {
		read()
		runtime.GC()
	}
	runtime.GC()
	before := count()
	for i := 0; i < 30; i++ {
		read()
	}
	runtime.GC()
	after := count()
	if after > before+4 {
		t.Fatalf("event handles retained: %d -> %d", before, after)
	}
}
