package server

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"

	log "github.com/sirupsen/logrus"
)

var (
	//go:embed ssh2incus-sftp-server-arm64.gz
	sftpServerArm64Bytes []byte
	//go:embed ssh2incus-sftp-server-amd64.gz
	sftpServerAmd64Bytes []byte

	sftpServerBinName = "/bin/ssh2incus-sftp-server"
)

func init() {
	reader := bytes.NewReader(sftpServerArm64Bytes)
	gzreader, err := gzip.NewReader(reader)
	defer gzreader.Close()
	if err != nil {
		log.Fatal(err)
	}

	sftpServerArm64Bytes, err = io.ReadAll(gzreader)
	if err != nil {
		log.Fatal(err)
	}
	gzreader.Close()

	reader = bytes.NewReader(sftpServerAmd64Bytes)
	gzreader, err = gzip.NewReader(reader)
	defer gzreader.Close()
	if err != nil {
		log.Fatal(err)
	}

	sftpServerAmd64Bytes, err = io.ReadAll(gzreader)
	if err != nil {
		log.Fatal(err)
	}
	gzreader.Close()
}
