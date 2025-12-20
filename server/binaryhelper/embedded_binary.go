package binaryhelper

import (
	"fmt"
	"strings"
)

// EmbeddedBinary wraps embedded artifacts for different architectures.
type EmbeddedBinary struct {
	name  string
	arm64 []byte
	amd64 []byte
}

// NewEmbeddedBinary validates the embedded bytes and returns a helper.
func NewEmbeddedBinary(name string, arm64Bytes, amd64Bytes []byte) *EmbeddedBinary {
	if name == "" {
		panic("embedded binary name must not be empty")
	}

	if len(arm64Bytes) == 0 {
		panic("arm64 binary bytes are empty")
	}
	if len(amd64Bytes) == 0 {
		panic("amd64 binary bytes are empty")
	}

	return &EmbeddedBinary{
		name:  name,
		arm64: arm64Bytes,
		amd64: amd64Bytes,
	}
}

// BinName returns the installed path for the embedded binary.
func (b *EmbeddedBinary) BinName() string {
	return b.name
}

// BinBytes returns the binary bytes that correspond to the requested architecture.
func (b *EmbeddedBinary) BinBytes(arch string) ([]byte, error) {
	switch normalized, ok := normalizeArch(arch); {
	case !ok:
		return nil, fmt.Errorf("unsupported arch: %s", arch)
	case normalized == "arm64":
		return b.arm64, nil
	default:
		return b.amd64, nil
	}
}

func normalizeArch(arch string) (string, bool) {
	switch strings.ToLower(arch) {
	case "arm64", "aarch64":
		return "arm64", true
	case "amd64", "x86_64", "x64", "x86-64", "x86":
		return "amd64", true
	default:
		return "", false
	}
}
