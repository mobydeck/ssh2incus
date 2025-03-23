package buffer

import (
	"bufio"
	"bytes"
	"sync"
)

type OutputBuffer struct {
	buf   *bytes.Buffer
	lines []string
	*sync.Mutex
}

func NewOutputBuffer() *OutputBuffer {
	out := &OutputBuffer{
		buf:   &bytes.Buffer{},
		lines: []string{},
		Mutex: &sync.Mutex{},
	}
	return out
}

func (b *OutputBuffer) Write(p []byte) (n int, err error) {
	b.Lock()
	n, err = b.buf.Write(p) // and bytes.Buffer implements io.Writer
	b.Unlock()
	return // implicit
}

func (b *OutputBuffer) Close() error {
	return nil
}

func (b *OutputBuffer) Lines() []string {
	b.Lock()
	s := bufio.NewScanner(b.buf)
	for s.Scan() {
		b.lines = append(b.lines, s.Text())
	}
	b.Unlock()
	return b.lines
}
