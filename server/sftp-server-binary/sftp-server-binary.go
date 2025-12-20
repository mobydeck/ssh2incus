package sftp_server_binary

import (
	_ "embed"

	"ssh2incus/server/binaryhelper"
)

var (
	//go:embed bin/ssh2incus-sftp-server-arm64.gz
	arm64Bytes []byte
	//go:embed bin/ssh2incus-sftp-server-amd64.gz
	amd64Bytes []byte

	bin = binaryhelper.NewEmbeddedBinary("/bin/ssh2incus-sftp-server", arm64Bytes, amd64Bytes)
)

func BinName() string {
	return bin.BinName()
}

func BinBytes(arch string) ([]byte, error) {
	return bin.BinBytes(arch)
}
