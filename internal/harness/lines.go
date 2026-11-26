package harness

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"time"
)

// maxText is the longest text an event carries. The live view does not need
// a whole file or a whole command output.
const maxText = 200

// maxLine is the longest stream line that is parsed. A longer line is
// skipped and counted, so one huge tool result cannot use all memory.
const maxLine = 8 << 20

// eachLine calls fn for every non-empty line of r. It never fails on a long
// line: a line over maxLine is dropped and reported through tooLong. The
// error is only a read error other than the end of the stream.
func eachLine(r io.Reader, fn func(line []byte), tooLong func()) error {
	br := bufio.NewReaderSize(r, 64<<10)
	var buf []byte
	dropping := false
	for {
		chunk, err := br.ReadSlice('\n')
		if len(chunk) > 0 && !dropping {
			if len(buf)+len(chunk) > maxLine {
				dropping = true
				buf = buf[:0]
			} else {
				buf = append(buf, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if len(chunk) > 0 || err == nil {
			switch {
			case dropping:
				tooLong()
			default:
				if line := bytes.TrimSpace(buf); len(line) > 0 {
					fn(line)
				}
			}
			buf, dropping = buf[:0], false
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// clip shortens text for an event and keeps it on one line.
func clip(s string) string {
	out := make([]rune, 0, maxText)
	n := 0
	for _, r := range s {
		if n >= maxText {
			return string(out) + "..."
		}
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		out = append(out, r)
		n++
	}
	return string(out)
}

// clock returns now, or time.Now when now is nil.
func clock(now func() time.Time) func() time.Time {
	if now == nil {
		return time.Now
	}
	return now
}
