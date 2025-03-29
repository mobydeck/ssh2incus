package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"syscall"

	"github.com/pkg/sftp"
)

var stderr = io.Discard

func main() {
	var (
		help        bool
		readOnly    bool
		debugStderr bool
		debugLevel  string
		startDir    string
		umask       int
		options     []sftp.ServerOption
	)

	flag.BoolVar(&readOnly, "R", readOnly, "read-only server")
	flag.BoolVar(&debugStderr, "e", debugStderr, "debug to stderr")
	flag.StringVar(&startDir, "d", startDir, "change root directory")
	flag.IntVar(&umask, "u", umask, "explicit umask")
	flag.StringVar(&debugLevel, "l", debugLevel, "debug level (ignored)")
	flag.BoolVar(&help, "h", help, "print help")
	flag.Parse()

	if help {
		flag.Usage()
		exit(nil)
	}

	if debugStderr {
		stderr = os.Stderr
	}

	if err := syscall.Chroot(startDir); err != nil {
		exit(err)
	}

	home, ok := os.LookupEnv("HOME")
	if !ok {
		exit(errors.New("HOME environment variable not set"))
	}

	gid, err := toInt(os.LookupEnv("GID"))
	if err != nil {
		exit(errors.New("GID environment variable not set"))
	}

	uid, err := toInt(os.LookupEnv("UID"))
	if err != nil {
		exit(errors.New("UID environment variable not set"))
	}

	if err = syscall.Chdir(home); err != nil {
		exit(err)
	}

	if err = syscall.Setgid(gid); err != nil {
		exit(err)
	}

	if err = syscall.Setuid(uid); err != nil {
		exit(err)
	}

	syscall.Umask(umask)

	options = append(options, sftp.WithDebug(stderr))

	if readOnly {
		options = append(options, sftp.ReadOnly())
	}

	server, err := sftp.NewServer(
		struct {
			io.Reader
			io.WriteCloser
		}{
			os.Stdin,
			os.Stdout,
		},
		options...,
	)
	if err != nil {
		exit(fmt.Errorf("sftp server could not initialize: %v", err))
	}

	if err = server.Serve(); err != nil {
		exit(fmt.Errorf("sftp server completed with error: %v", err))
	}
}

func exit(err error) {
	if err != nil {
		fmt.Fprintln(stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func toInt(s string, ok bool) (int, error) {
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}

	return i, nil
}
