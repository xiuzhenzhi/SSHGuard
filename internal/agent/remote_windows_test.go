package agent

import (
	"context"
	"encoding/xml"
	"fmt"
	"testing"
	"time"
)

func TestWindowsRemoteQueries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// A deliberately unreachable record ID checks native XPath support without
	// retrieving or logging any actual machine authentication records.
	for _, tc := range []struct{ channel, query string }{
		{rdpSessionChannel, rdpReconnectQuery},
		{"Security", "System[(EventID=4624 or EventID=4625)] and EventData[Data[@Name='LogonType']='2' or Data[@Name='LogonType']='10' or Data[@Name='LogonType']='11' or Data[@Name='LogonType']='12']"},
		{"Application", "System[Provider[@Name='VNC Server'] or Provider[@Name='UltraVNC']]"},
	} {
		_, err := readWindowsEvents(ctx, "qe", tc.channel, "/f:RenderedXml", "/e:Events", "/uni:true", "/c:1", "/q:*[System[EventRecordID > 9007199254740991] and ("+tc.query+")]")
		if err != nil {
			t.Fatalf("%s native XPath failed: %v", tc.channel, err)
		}
	}
}

func TestWindowsRemoteLogonTypes(t *testing.T) {
	for _, tc := range []struct {
		logon string
		id    int
		want  string
	}{{"10", 4624, "rdp.login"}, {"10", 4625, "rdp.login_failed"}, {"2", 4624, "console.login"}, {"11", 4624, "console.login"}, {"3", 4625, ""}, {"5", 4624, ""}, {"7", 4624, ""}, {"10", 4634, ""}} {
		raw := fmt.Sprintf(`<Event><System><Provider Name="Microsoft-Windows-Security-Auditing"/><EventID>%d</EventID><EventRecordID>42</EventRecordID><TimeCreated SystemTime="2026-11-03T10:00:00Z"/></System><EventData><Data Name="LogonType">%s</Data><Data Name="TargetUserName">Administrator</Data><Data Name="TargetDomainName">HOST</Data><Data Name="IpAddress">::ffff:192.0.2.8</Data><Data Name="IpPort">48200</Data></EventData></Event>`, tc.id, tc.logon)
		var record windowsEvent
		if err := xml.Unmarshal([]byte(raw), &record); err != nil {
			t.Fatal(err)
		}
		e, ok := parseWindowsLogin(record, time.Now())
		if ok != (tc.want != "") || e.Type != tc.want {
			t.Fatal(tc, e)
		}
		if tc.logon == "2" || tc.logon == "11" {
			if e.Username != "" || e.IP != "" || e.Port != 0 {
				t.Fatal("console privacy", e)
			}
		}
		if ok && tc.logon == "10" && (e.Username != `HOST\Administrator` || e.IP != "192.0.2.8" || e.Port != 48200) {
			t.Fatal("RDP fields", e)
		}
	}
}

func TestWindowsReconnect(t *testing.T) {
	at := time.Date(2026, 11, 11, 10, 1, 41, 0, time.UTC)
	for _, tc := range []struct {
		address, ip string
		want        bool
	}{
		{"::ffff:192.0.2.8", "192.0.2.8", true},
		{"2001:db8::8", "2001:db8::8", true},
		{"", "", true}, {"-", "", true}, {"0.0.0.0", "", true},
		{"LOCAL", "", false},
	} {
		raw := fmt.Sprintf(`<Event xmlns="http://schemas.microsoft.com/win/2004/08/events/event"><System><Provider Name="Microsoft-Windows-TerminalServices-LocalSessionManager"/><EventID>25</EventID><EventRecordID>42</EventRecordID><TimeCreated SystemTime="2026-11-11T10:01:41Z"/></System><UserData><EventXML xmlns="Event_NS"><User>HOST\Administrator</User><SessionID>2</SessionID><Address>%s</Address></EventXML></UserData></Event>`, tc.address)
		var record windowsEvent
		if err := xml.Unmarshal([]byte(raw), &record); err != nil {
			t.Fatal(err)
		}
		event, ok := parseWindowsReconnect(record, at)
		if ok != tc.want {
			t.Fatalf("address %q: accepted=%v", tc.address, ok)
		}
		if !ok {
			continue
		}
		if event.Type != "rdp.login" || event.Outcome != "success" || event.Username != `HOST\Administrator` || event.IP != tc.ip || event.Port != 0 || !event.At.Equal(at) {
			t.Fatalf("unexpected reconnect: %+v", event)
		}
		again, _ := parseWindowsReconnect(record, at)
		if event.ID != again.ID {
			t.Fatal("unstable event ID")
		}
		record.System.RecordID++
		next, _ := parseWindowsReconnect(record, at)
		if event.ID == next.ID {
			t.Fatal("separate reconnects share event ID")
		}
		for _, id := range []int{21, 24, 40, 4624} {
			record.System.EventID = id
			if _, ok := parseWindowsReconnect(record, at); ok {
				t.Fatalf("accepted event %d", id)
			}
		}
		record.System.EventID = 25
		record.System.Provider.Name = "Other"
		if _, ok := parseWindowsReconnect(record, at); ok {
			t.Fatal("accepted wrong provider")
		}
	}
}
