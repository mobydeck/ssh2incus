package server

import (
	"fmt"

	"ssh2incus/pkg/ssh"
)

func defaultSubsystemHandler(s ssh.Session) {
	s.Write([]byte(fmt.Sprintf("%s subsytem not implemented\n", s.Subsystem())))
	s.Exit(ExitCodeNotImplemented)
}
