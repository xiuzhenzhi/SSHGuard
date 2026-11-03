package agent

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	eventDLL  = windows.NewLazySystemDLL("wevtapi.dll")
	evtQuery  = eventDLL.NewProc("EvtQuery")
	evtNext   = eventDLL.NewProc("EvtNext")
	evtRender = eventDLL.NewProc("EvtRender")
	evtClose  = eventDLL.NewProc("EvtClose")
)

// Keep the collector's query options, but read locally through Windows Event
// Log API: no child process, pipe, provider message DLL or whole-batch XML copy.
// EventData contains the original OpenSSH / VNC text, independent of locale.
func readWindowsEvents(ctx context.Context, args ...string) ([]windowsEvent, error) {
	if len(args) < 2 || args[0] != "qe" {
		return nil, errors.New("invalid event query")
	}
	channel, query, limit, flags := args[1], "*", 100, uintptr(1|0x100)
	for _, arg := range args[2:] {
		switch {
		case strings.HasPrefix(arg, "/q:"):
			query = arg[3:]
		case strings.HasPrefix(arg, "/c:"):
			var err error
			limit, err = strconv.Atoi(arg[3:])
			if err != nil || limit < 1 || limit > 100 {
				return nil, errors.New("invalid event query limit")
			}
		case arg == "/rd:true":
			flags = 1 | 0x200
		case arg == "/rd:false":
			flags = 1 | 0x100
		case arg == "/f:RenderedXml" || arg == "/e:Events" || arg == "/uni:true":
		default:
			return nil, errors.New("unsupported event query option")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	channelPtr, err := windows.UTF16PtrFromString(channel)
	if err != nil {
		return nil, err
	}
	queryPtr, err := windows.UTF16PtrFromString(query)
	if err != nil {
		return nil, err
	}
	// Windows requires a query handle to be used on its creating thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	handle, _, callErr := evtQuery.Call(0, uintptr(unsafe.Pointer(channelPtr)), uintptr(unsafe.Pointer(queryPtr)), flags)
	if handle == 0 {
		return nil, fmt.Errorf("%s event log unavailable; check service read permission: %w", channel, callErr)
	}
	defer evtClose.Call(handle)
	var records []windowsEvent
	var buffer []uint16 // Lazy: idle polls allocate no rendering buffer.
	total := 0
	for len(records) < limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var event uintptr
		var returned uint32
		ok, _, callErr := evtNext.Call(handle, 1, uintptr(unsafe.Pointer(&event)), 200, 0, uintptr(unsafe.Pointer(&returned)))
		if ok == 0 {
			if errors.Is(callErr, windows.ERROR_NO_MORE_ITEMS) {
				break
			}
			if errors.Is(callErr, windows.ERROR_TIMEOUT) {
				continue
			}
			return nil, fmt.Errorf("%s event query failed: %w", channel, callErr)
		}
		if returned != 1 || event == 0 {
			return nil, errors.New("invalid event query result")
		}
		record, size, err := renderWindowsEvent(event, &buffer)
		evtClose.Call(event)
		if err != nil {
			return nil, err
		}
		total += size
		if total > 4<<20 {
			return nil, errors.New("Windows event batch exceeds limit")
		}
		records = append(records, record)
	}
	return records, nil
}

func renderWindowsEvent(handle uintptr, buffer *[]uint16) (windowsEvent, int, error) {
	var record windowsEvent
	if len(*buffer) == 0 {
		*buffer = make([]uint16, 4096)
	}
	for {
		var used, properties uint32
		ok, _, err := evtRender.Call(0, handle, 1, uintptr(len(*buffer)*2), uintptr(unsafe.Pointer(&(*buffer)[0])), uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&properties)))
		if ok == 0 {
			if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
				return record, 0, fmt.Errorf("Windows event rendering failed: %w", err)
			}
			if used <= uint32(len(*buffer)*2) || used > 256<<10 {
				return record, 0, errors.New("Windows event exceeds render limit")
			}
			*buffer = make([]uint16, (used+1)/2)
			continue
		}
		if used < 2 || used%2 != 0 || used > uint32(len(*buffer)*2) {
			return record, 0, errors.New("invalid Windows event render length")
		}
		text := windows.UTF16ToString((*buffer)[:used/2])
		if err := xml.NewDecoder(strings.NewReader(text)).Decode(&record); err != nil {
			return record, 0, errors.New("invalid Windows event XML")
		}
		return record, int(used), nil
	}
}
