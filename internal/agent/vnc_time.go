package agent

import "time"

// Choose a layout from the timestamp prefix instead of creating ParseErrors for
// every candidate layout on every continuation line. Do not scan user payloads.
func vncTimestamp(line string, now time.Time) (time.Time, bool) {
	for i := 0; i < min(len(line), 128); i++ {
		s := line[i:]
		layout, value := "", ""
		switch {
		case len(s) >= 19 && digit(s[0]) && s[4] == '-' && s[7] == '-' && (s[10] == ' ' || s[10] == 'T') && s[13] == ':' && s[16] == ':':
			end := 19
			if len(s) > end && s[end] == '.' {
				end++
				for end < len(s) && digit(s[end]) {
					end++
				}
			}
			layout = "2006-01-02 15:04:05.999999999"
			if s[10] == 'T' {
				layout = "2006-01-02T15:04:05.999999999"
			}
			if len(s) > end {
				if s[end] == 'Z' {
					end++
					layout += "Z07:00"
				} else if s[end] == '+' || s[end] == '-' {
					if len(s) >= end+6 && s[end+3] == ':' {
						end += 6
						layout += "Z07:00"
					} else if len(s) >= end+5 {
						end += 5
						layout += "-0700"
					}
				}
			}
			value = s[:end]
		case len(s) >= 19 && digit(s[0]) && s[2] == '/' && s[5] == '/' && s[10] == ' ' && s[13] == ':' && s[16] == ':':
			layout, value = "02/01/2006 15:04:05", s[:19]
		case len(s) >= 24 && asciiLetter(s[0]) && s[3] == ' ' && s[7] == ' ' && s[10] == ' ' && s[13] == ':' && s[16] == ':' && s[19] == ' ':
			layout, value = "Mon Jan _2 15:04:05 2006", s[:24]
		default:
			continue
		}
		at, err := time.ParseInLocation(layout, value, now.Location())
		if err == nil {
			return at, true
		}
	}
	return time.Time{}, false
}

func digit(c byte) bool { return c >= '0' && c <= '9' }
