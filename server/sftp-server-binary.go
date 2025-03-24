package server

import (
	_ "embed"
)

var (
	//go:embed bin/ssh2incus-sftp-server-arm64.gz
	sftpServerArm64Bytes []byte
	//go:embed bin/ssh2incus-sftp-server-amd64.gz
	sftpServerAmd64Bytes []byte

	sftpServerBinName = "/bin/ssh2incus-sftp-server"
)
