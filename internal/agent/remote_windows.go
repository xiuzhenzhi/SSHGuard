package agent

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

func platformJournalAvailable() bool { return false }

func parseWindowsLogin(record windowsEvent, at time.Time) (SSHEvent, bool) {
	if record.System.Provider.Name != "Microsoft-Windows-Security-Auditing" || (record.System.EventID != 4624 && record.System.EventID != 4625) {
		return SSHEvent{}, false
	}
	fields := map[string]string{}
	for _, value := range record.Data.Values {
		fields[value.Name] = value.Value
	}
	protocol := ""
	switch fields["LogonType"] {
	case "10", "12":
		protocol = "rdp"
	case "2", "11":
		protocol = "console"
	default:
		return SSHEvent{}, false // Type 3 also includes SMB; it cannot reliably identify RDP/NLA failures.
	}
	name := fields["TargetUserName"]
	if strings.HasPrefix(name, "DWM-") || strings.HasPrefix(name, "UMFD-") || strings.HasSuffix(name, "$") || name == "SYSTEM" || name == "LOCAL SERVICE" || name == "NETWORK SERVICE" {
		return SSHEvent{}, false
	}
	outcome := "success"
	if record.System.EventID == 4625 {
		outcome = "failure"
	}
	e := loginEvent(protocol, outcome, fmt.Sprintf("windows|Security|%d|%s", record.System.RecordID, record.System.Time.Value), at)
	if protocol == "rdp" {
		if name != "-" {
			e.Username = name
		}
		if domain := fields["TargetDomainName"]; domain != "" && domain != "-" && e.Username != "" {
			e.Username = domain + `\` + e.Username
		}
		if addr, err := netip.ParseAddr(fields["IpAddress"]); err == nil && !addr.IsUnspecified() {
			e.IP = addr.Unmap().String()
		}
		if port, err := strconv.Atoi(fields["IpPort"]); err == nil && port > 0 && port <= 65535 && e.IP != "" {
			e.Port = port
		}
	}
	return e, true
}

func collectWindowsRemote(ctx context.Context, s *spool, channel, query string) (time.Time, error) {
	source := "windows:" + channel
	previous, err := s.positionFor(source)
	if err != nil {
		return time.Time{}, err
	}
	base := []string{"qe", channel, "/f:RenderedXml", "/e:Events", "/uni:true"}
	if previous.Kind == "" {
		records, err := readWindowsEvents(ctx, append(base, "/rd:true", "/c:1")...)
		if err != nil {
			return time.Time{}, fmt.Errorf("%s event log unavailable; check service read permission and audit policy", channel)
		}
		initial := cursor{Kind: source, Position: "0", At: time.Now().UTC()}
		if len(records) > 0 {
			initial.Position = strconv.FormatUint(records[0].System.RecordID, 10)
		}
		return time.Time{}, s.appendFor(source, nil, initial)
	}
	position, err := strconv.ParseUint(previous.Position, 10, 64)
	if err != nil {
		return time.Time{}, errors.New("invalid remote-login event cursor")
	}
	filter := fmt.Sprintf("/q:*[System[EventRecordID > %d] and (%s)]", position, query)
	records, err := readWindowsEvents(ctx, append(base, "/rd:false", "/c:100", filter)...)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s event log unavailable; check service read permission and audit policy", channel)
	}
	if len(records) == 0 {
		// Check for cleared logs even when no matching login events exist.
		if time.Since(previous.At) < time.Minute {
			return time.Time{}, nil
		}
		latest, err := readWindowsEvents(ctx, append(base, "/rd:true", "/c:1")...)
		if err != nil {
			return time.Time{}, err
		}
		if len(latest) == 0 || latest[0].System.RecordID < position {
			previous.Position = "0"
			if err = s.appendFor(source, nil, previous); err != nil {
				return time.Time{}, err
			}
			return time.Time{}, fmt.Errorf("%s log cleared; possible remote-login collection gap", channel)
		}
		previous.At = time.Now().UTC()
		return time.Time{}, s.appendFor(source, nil, previous)
	}
	next, last := previous, time.Time{}
	events := []SSHEvent{}
	for _, record := range records {
		at, err := time.Parse(time.RFC3339Nano, record.System.Time.Value)
		if err != nil {
			return time.Time{}, errors.New("invalid remote-login event time")
		}
		var event SSHEvent
		var ok bool
		if channel == "Security" {
			event, ok = parseWindowsLogin(record, at)
		} else {
			format := ""
			switch record.System.Provider.Name {
			case "VNC Server":
				format = "realvnc"
			case "UltraVNC":
				format = "ultravnc"
			}
			event, ok = parseVNC(format, record.message(), fmt.Sprintf("windows|%s|%d|%s", channel, record.System.RecordID, record.System.Time.Value), at)
		}
		if ok {
			events = append(events, event)
			last = at
		}
		next.Position = strconv.FormatUint(record.System.RecordID, 10)
		next.At = time.Now().UTC()
	}
	return last, s.appendFor(source, events, next)
}

func collectRemotePlatform(ctx context.Context, s *spool) (time.Time, error) {
	last, e1 := collectWindowsRemote(ctx, s, "Security", "System[(EventID=4624 or EventID=4625)] and EventData[Data[@Name='LogonType']='2' or Data[@Name='LogonType']='10' or Data[@Name='LogonType']='11' or Data[@Name='LogonType']='12']")
	at, e2 := collectWindowsRemote(ctx, s, "Application", "System[Provider[@Name='VNC Server'] or Provider[@Name='UltraVNC']]")
	if at.After(last) {
		last = at
	}
	return last, errors.Join(e1, e2)
}
