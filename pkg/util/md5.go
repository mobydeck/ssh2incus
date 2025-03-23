package util

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

func Sha256Bytes(b []byte) string {
	return sha256hash(bytes.NewReader(b))
}

func sha256hash(b io.Reader) string {
	h := sha256.New()
	_, err := io.Copy(h, b)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func Md5Bytes(b []byte) string {
	return md5hash(bytes.NewReader(b))
}

func Md5File(file string) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()

	return md5hash(f)
}

func md5hash(b io.Reader) string {
	h := md5.New()
	_, err := io.Copy(h, b)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
