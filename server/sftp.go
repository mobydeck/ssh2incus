package server

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"ssh2incus/pkg/incus"
	"ssh2incus/pkg/util"
	"ssh2incus/pkg/util/ssh"

	log "github.com/sirupsen/logrus"
)

func sftpSubsystemHandler(s ssh.Session) {
	lu, ok := s.Context().Value("LoginUser").(LoginUser)
	if !ok || !lu.IsValid() {
		log.Errorf("invalid connection data for %#v", lu)
		io.WriteString(s, "invalid connection data")
		s.Exit(ExitCodeInvalidLogin)
		return
	}
	log.Debugf("sftp: connecting %#v", lu)

	server, err := NewIncusServer()
	if err != nil {
		log.Errorf("failed to initialize incus client: %v", err)
		io.WriteString(s, "invalid connection data")
		s.Exit(ExitCodeInvalidLogin)
		return
	}

	// subsystem needs own context
	err = server.Connect(context.Background())
	if err != nil {
		log.Errorf("failed to connect to incus: %v", err)
		s.Exit(ExitCodeConnectionError)
		return
	}
	defer server.Disconnect()

	if !lu.IsDefaultProject() {
		err = server.UseProject(lu.Project)
		if err != nil {
			log.Errorf("using project %s error: %v", lu.Project, err)
			io.WriteString(s, fmt.Sprintf("unknown project %s\n", lu.Project))
			s.Exit(ExitCodeInvalidProject)
			return
		}
	}

	meta, _, err := server.GetInstanceMeta(lu.Instance)
	if err != nil {
		log.Errorf("cannot get instance meta: %s", err)
		io.WriteString(s, fmt.Sprintf("cannot get instance meta\n"))
		s.Exit(ExitCodeMetaError)
		return
	}
	log.Debugf("sftp: instance meta: %#v", meta)

	var sftpServerBinBytes []byte
	switch meta.Architecture {
	case "arm64", "aarch64":
		sftpServerBinBytes = sftpServerArm64Bytes
	case "amd64", "x86_64", "x64", "x86-64", "x86":
		sftpServerBinBytes = sftpServerAmd64Bytes
	default:
		log.Errorf("unsupported architecture: %s", meta.Architecture)
		io.WriteString(s, fmt.Sprintf("unsupported architecture: %s\n", meta.Architecture))
		s.Exit(ExitCodeArchitectureError)
		return
	}
	sftpServerBinBytes, err = util.Ungz(sftpServerBinBytes)
	if err != nil {
		log.Errorf("failed to ungzip sftp-server: %s", err)
		io.WriteString(s, fmt.Sprintf("failed to prepare sftp-server\n"))
		s.Exit(ExitCodeInternalError)
	}

	if !server.FileExists(lu.Project, lu.Instance, sftpServerBinName, util.Md5Bytes(sftpServerBinBytes), true) {
		err = server.UploadBytes(lu.Project, lu.Instance, sftpServerBinName, bytes.NewReader(sftpServerBinBytes), 0, 0, 0755)
		if err != nil {
			log.Errorf("upload failed: %v", err)
			io.WriteString(s, fmt.Sprintf("sftp-server is not available on %s.%s\n", lu.Instance, lu.Project))
			s.Exit(ExitCodeConnectionError)
			return
		}
		log.Debugf("sftp: uploaded %s to %s.%s", sftpServerBinName, lu.Instance, lu.Project)
	}
	sftpServerBinBytes = nil

	var iu *incus.InstanceUser
	if lu.InstanceUser != "" {
		iu = server.GetInstanceUser(lu.Project, lu.Instance, lu.InstanceUser)
	}

	if iu == nil {
		io.WriteString(s, "not found user or instance\n")
		log.Errorf("sftp: not found instance user for %#v", lu)
		s.Exit(ExitCodeInvalidLogin)
		return
	}

	log.Debugf("sftp: found instance user %s [%d %d]", iu.User, iu.Uid, iu.Gid)

	stdin, inWrite := io.Pipe()
	errRead, stderr := io.Pipe()

	go func(s ssh.Session, w io.WriteCloser) {
		defer w.Close()
		io.Copy(w, s)
	}(s, inWrite)

	go func(s ssh.Session, e io.ReadCloser) {
		defer e.Close()
		io.Copy(s.Stderr(), e)
	}(s, errRead)

	chroot := "/"
	home := iu.Dir
	uid := 0
	gid := 0
	if iu.Uid != 0 {
		chroot = iu.Dir
		home = "/"
	}
	cmd := fmt.Sprintf("%s -e -d %s", sftpServerBinName, chroot)

	env := make(map[string]string)
	env["USER"] = iu.User
	env["UID"] = fmt.Sprintf("%d", iu.Uid)
	env["GID"] = fmt.Sprintf("%d", iu.Gid)
	env["HOME"] = home

	log.Debugf("sftp cmd: %v", cmd)
	log.Debugf("sftp env: %v", env)

	ie := server.NewInstanceExec(incus.InstanceExec{
		Instance: lu.Instance,
		Cmd:      cmd,
		Env:      env,
		Stdin:    stdin,
		Stdout:   s,
		Stderr:   stderr,
		User:     uid,
		Group:    gid,
	})

	ret, err := ie.Exec()
	if err != nil {
		io.WriteString(s, "sftp connection failed\n")
		log.Errorf("sftp: connection failed: %s", err)
	}

	s.Exit(ret)
}
