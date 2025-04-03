package buffer

import (
	"bufio"
	"bytes"
	"sync"
)

type BytesBuffer struct {
	buf   *bytes.Buffer
	lines [][]byte
	*sync.Mutex
}

func NewBytesBuffer() *BytesBuffer {
	out := &BytesBuffer{
		buf:   &bytes.Buffer{},
		lines: [][]byte{},
		Mutex: &sync.Mutex{},
	}
	return out
}

func (b *BytesBuffer) Write(p []byte) (n int, err error) {
	b.Lock()
	n, err = b.buf.Write(p) // and bytes.Buffer implements io.Writer
	b.Unlock()
	return // implicit
}

func (b *BytesBuffer) Close() error {
	b.byteLines()
	return nil
}

func (b *BytesBuffer) Lines() [][]byte {
	if b.lines != nil {
		return b.lines
	}
	b.Lock()
	b.byteLines()
	b.Unlock()
	return b.lines
}

func (b *BytesBuffer) byteLines() {
	s := bufio.NewScanner(b.buf)
	for s.Scan() {
		b.lines = append(b.lines, s.Bytes())
	}
}

func (b *BytesBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

func (b *BytesBuffer) Size() int64 {
	return int64(b.buf.Len())
}
