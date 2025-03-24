package util

import (
	"bytes"
	"compress/gzip"
	"io"
)

func Ungz(b []byte) ([]byte, error) {
	reader := bytes.NewReader(b)
	gzreader, err := gzip.NewReader(reader)
	if err != nil {
		return nil, err
	}
	defer gzreader.Close()

	b, err = io.ReadAll(gzreader)
	return b, err
}
