package agent

import (
	"bufio"
)

// TightVNC on Windows writes UTF-16LE. Keep offsets in bytes and do not
// consume half a character/newline when the writer has not finished a line.
func readLoginLine(reader *bufio.Reader, wide bool, scratch []byte) ([]byte, error) {
	if !wide {
		return reader.ReadSlice('\n')
	}
	line := scratch[:0]
	for len(line) < 65536 {
		lo, err := reader.ReadByte()
		if err != nil {
			return line, err
		}
		hi, err := reader.ReadByte()
		if err != nil {
			return line, err
		}
		line = append(line, lo, hi)
		if lo == '\n' && hi == 0 {
			return line, nil
		}
	}
	return line, bufio.ErrBufferFull
}
