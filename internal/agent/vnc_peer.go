package agent

import "strings"

func vncPeerAddress(lower string) (string, int) {
	for i := 0; i < len(lower); i++ {
		if i > 0 && (asciiLetter(lower[i-1]) || digit(lower[i-1]) || lower[i-1] == '_') {
			continue
		}
		start := i
		switch {
		case strings.HasPrefix(lower[i:], "from"):
			start += 4
			if strings.HasPrefix(lower[start:], " client") {
				start += 7
			}
		case strings.HasPrefix(lower[i:], "passed by"):
			start += 9
		default:
			continue
		}
		if start >= len(lower) || !sshSpace(lower[start]) {
			continue
		}
		for start < len(lower) && sshSpace(lower[start]) {
			start++
		}
		end := start
		if end < len(lower) && lower[end] == '[' {
			end++
			for end < len(lower) && sshIPByte(lower[end]) {
				end++
			}
			if end >= len(lower) || lower[end] != ']' {
				continue
			}
			end++
		}
		for end < len(lower) && sshIPByte(lower[end]) {
			end++
		}
		if end > start {
			// Unicode case folding can change byte lengths. Slice the same
			// string we scanned; IP literals are case insensitive.
			return peerAddress(lower[start:end])
		}
	}
	return "", 0
}
