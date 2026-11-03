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
