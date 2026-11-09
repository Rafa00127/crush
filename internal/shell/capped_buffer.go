package shell

import (
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// maxKeptOutput bounds one output stream: plenty for the spill file, and it
// caps what each retained finished job holds.
const maxKeptOutput = 1 << 20

// cappedBuffer keeps the first and last limit/2 bytes of a stream in fixed
// memory, so a command that never stops writing cannot exhaust it. It is
// safe for concurrent use.
type cappedBuffer struct {
	mu      sync.Mutex
	half    int
	head    []byte
	ring    []byte // The latest bytes past the head, oldest at next once full.
	next    int
	dropped int64
	// written counts every byte ever passed to Write, including those
	// dropped from the buffer, so callers can tell "still producing
	// output" from "silent" without reading the content.
	written   int64
	lastWrite time.Time
}

func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{half: limit / 2}
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if n > 0 {
		b.written += int64(n)
		b.lastWrite = time.Now()
	}
	take := min(b.half-len(b.head), len(p))
	b.head = append(b.head, p[:take]...)
	p = p[take:]
	if fill := min(b.half-len(b.ring), len(p)); fill > 0 {
		b.ring = append(b.ring, p[:fill]...)
		p = p[fill:]
	}
	if len(p) > b.half {
		b.dropped += int64(len(p) - b.half)
		p = p[len(p)-b.half:]
	}
	for len(p) > 0 {
		c := copy(b.ring[b.next:], p)
		b.dropped += int64(c)
		b.next = (b.next + c) % b.half
		p = p[c:]
	}
	return n, nil
}

// Written reports how many bytes the stream has produced in total, including
// bytes dropped from the retained window.
func (b *cappedBuffer) Written() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.written
}

// LastWrite reports when the stream last received bytes, or the zero time if it
// never has.
func (b *cappedBuffer) LastWrite() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastWrite
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	older, newer := b.ring[b.next:], b.ring[:b.next]
	if b.dropped == 0 {
		var sb strings.Builder
		sb.Grow(len(b.head) + len(b.ring))
		sb.Write(b.head)
		sb.Write(older)
		sb.Write(newer)
		return sb.String()
	}

	// Trim partial runes at the seams, at most UTFMax bytes so binary survives.
	head := b.head
	start := len(head) - 1
	for start > 0 && len(head)-start < utf8.UTFMax && !utf8.RuneStart(head[start]) {
		start--
	}
	if start >= 0 && !utf8.FullRune(head[start:]) {
		head = head[:start]
	}
	skip := 0
	for skip < len(b.ring) && skip < utf8.UTFMax-1 && !utf8.RuneStart(ringByte(older, newer, skip)) {
		skip++
	}
	dropped := b.dropped + int64(len(b.head)-len(head)+skip)
	if skip < len(older) {
		older = older[skip:]
	} else {
		newer = newer[skip-len(older):]
		older = nil
	}

	const prefix, suffix = "\n\n... [", " bytes truncated] ...\n\n"
	count := strconv.FormatInt(dropped, 10)
	var sb strings.Builder
	sb.Grow(len(head) + len(prefix) + len(count) + len(suffix) + len(older) + len(newer))
	sb.Write(head)
	sb.WriteString(prefix)
	sb.WriteString(count)
	sb.WriteString(suffix)
	sb.Write(older)
	sb.Write(newer)
	return sb.String()
}

// ringByte returns byte i of the tail, which is older followed by newer.
func ringByte(older, newer []byte, i int) byte {
	if i < len(older) {
		return older[i]
	}
	return newer[i-len(older)]
}
