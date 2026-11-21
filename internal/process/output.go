package process

import "strings"

const maxOutputBytes = 8 * 1024 * 1024

type outputBuffer struct {
	lines    []string
	pending  string
	base     int
	maxLines int
	bytes    int
}

func newOutputBuffer(maxLines int) *outputBuffer {
	if maxLines <= 0 {
		maxLines = 50000
	}
	return &outputBuffer{maxLines: maxLines}
}

func (b *outputBuffer) Append(data []byte) {
	if len(data) == 0 {
		return
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = b.pending + text
	parts := strings.Split(text, "\n")
	b.pending = parts[len(parts)-1]
	for _, line := range parts[:len(parts)-1] {
		b.lines = append(b.lines, line)
		b.bytes += len(line) + 1
	}
	b.trim()
}

func (b *outputBuffer) Flush() {
	if b.pending == "" {
		return
	}
	b.lines = append(b.lines, b.pending)
	b.bytes += len(b.pending)
	b.pending = ""
	b.trim()
}

func (b *outputBuffer) trim() {
	for len(b.lines) > b.maxLines || b.bytes > maxOutputBytes {
		if len(b.lines) == 0 {
			b.pending = ""
			b.bytes = 0
			return
		}
		b.bytes -= len(b.lines[0]) + 1
		b.lines = b.lines[1:]
		b.base++
	}
	if b.bytes < 0 {
		b.bytes = 0
	}
}

func (b *outputBuffer) visible() []string {
	if b.pending == "" {
		return b.lines
	}
	view := make([]string, len(b.lines)+1)
	copy(view, b.lines)
	view[len(b.lines)] = b.pending
	return view
}

func (b *outputBuffer) NewAvailable(cursor int) bool {
	return cursor < b.base+len(b.lines) || b.pending != ""
}

func (b *outputBuffer) Read(offset, length int, cursor *int) (string, int, int, bool) {
	view := b.visible()
	total := len(view)
	if length <= 0 {
		length = 1000
	}
	if length > 10000 {
		length = 10000
	}
	start := 0
	truncated := false
	switch {
	case offset == 0:
		start = *cursor - b.base
		if start < 0 {
			start = 0
			truncated = true
		}
		if start > total {
			start = total
		}
	case offset > 0:
		start = offset - 1 - b.base
		if start < 0 {
			start = 0
			truncated = true
		}
	case offset < 0:
		start = total + offset
		if start < 0 {
			start = 0
			truncated = true
		}
	}
	end := start + length
	if end > total {
		end = total
	}
	if end < start {
		end = start
	}
	if offset == 0 {
		*cursor = b.base + len(b.lines)
	}
	if end < total {
		truncated = true
	}
	return strings.Join(view[start:end], "\n"), total, end - start, truncated
}

func (b *outputBuffer) Discard(cursor *int) {
	*cursor = b.base + len(b.lines)
	b.pending = ""
}
