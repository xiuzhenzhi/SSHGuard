package agent

import (
	"regexp"
	"testing"
	"time"
)

// Retain the former implementation only as a reproducible allocation baseline.
func legacyVNCTimestamp(line string, now time.Time) (time.Time, bool) {
	value := legacyVNCDate.FindString(line)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999-0700", "2006-01-02 15:04:05.999999-0700", "2006-01-02 15:04:05.999999", "2006-01-02T15:04:05.999999", "02/01/2006 15:04:05", "Mon Jan _2 15:04:05 2006"} {
		if at, err := time.ParseInLocation(layout, value, now.Location()); err == nil {
			return at, true
		}
	}
	return time.Time{}, false
}

var legacyVNCDate = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?|\d{2}/\d{2}/\d{4} \d{2}:\d{2}:\d{2}|\w{3} \w{3} [ \d]\d \d{2}:\d{2}:\d{2} \d{4}`)

func BenchmarkVNCTimestamp(b *testing.B) {
	for _, sample := range []struct{ name, line string }{
		{"dated", "[123/456] 2026-11-03 14:12:01 : Authenticated with view-only password = 0"},
		{"continuation", " VNCSConnST: Server default pixel format depth 24 (32bpp) little-endian rgb888"},
	} {
		for _, impl := range []struct {
			name  string
			parse func(string, time.Time) (time.Time, bool)
		}{{"before", legacyVNCTimestamp}, {"after", vncTimestamp}} {
			b.Run(sample.name+"/"+impl.name, func(b *testing.B) {
				b.ReportAllocs()
				now := time.Now()
				for b.Loop() {
					impl.parse(sample.line, now)
				}
			})
		}
	}
}

func TestVNCTimestampCompatibility(t *testing.T) {
	now := time.Now()
	for _, line := range []string{"2026-11-03T14:12:01.123Z msg", "2026-11-03T14:12:01+08:00 msg", "2026-11-03T14:12:01+0800 msg", "2026-11-03 14:12:01.123-0700 msg", "03/11/2026 14:12:01 msg", "Tue Nov  3 14:12:01 2026", "[123/456] 2026-11-03 14:12:01:123 msg", "no time", "2026-99-99 99:99:99 msg"} {
		before, bok := legacyVNCTimestamp(line, now)
		after, aok := vncTimestamp(line, now)
		if bok != aok || !before.Equal(after) {
			t.Fatalf("timestamp changed for %q: %v %v / %v %v", line, before, bok, after, aok)
		}
	}
}
