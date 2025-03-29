package stdio_proxy_binary

import (
	_ "embed"
	"fmt"
)

var (
	//go:embed bin/ssh2incus-stdio-proxy-arm64.gz
	arm64Bytes []byte
	//go:embed bin/ssh2incus-stdio-proxy-amd64.gz
	amd64Bytes []byte

	binName = "/bin/ssh2incus-stdio-proxy"
)

const (
	Arm64 = "arm64"
	Amd64 = "amd64"
)

func init() {
	if len(arm64Bytes) == 0 {
		panic("arm64Bytes is empty")
	}
	if len(amd64Bytes) == 0 {
		panic("amd64Bytes is empty")
	}
}

func BinName() string {
	return binName
}

func BinBytes(arch string) ([]byte, error) {
	switch arch {
	case "arm64", "aarch64":
		return arm64Bytes, nil
	case "amd64", "x86_64", "x64", "x86-64", "x86":
		return amd64Bytes, nil
	default:
		return nil, fmt.Errorf("unsupported arch: %s", arch)
	}
}
