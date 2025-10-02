package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"ssh2incus/pkg/incus"
	"ssh2incus/pkg/ssh"
	"ssh2incus/pkg/util"
	sftp_server_binary "ssh2incus/server/sftp-server-binary"

	log "github.com/sirupsen/logrus"
)

const sftpSubsystem = "sftp"

func sftpSubsystemHandler(s ssh.Session) {
	log := log.WithField("session", s.Context().ShortSessionID())

	lu := LoginUserFromContext(s.Context())
	if !lu.IsValid() {
		log.Errorf("invalid login for %s", lu)
		io.WriteString(s, fmt.Sprintf("Invalid login for %q (%s)\n", lu.OrigUser, lu))
		s.Exit(ExitCodeInvalidLogin)
		return
	}
	log.Debugf("sftp: connecting %s", lu)

	client, err := NewDefaultIncusClientWithContext(s.Context())
	if err != nil {
		log.Error(err)
		s.Exit(ExitCodeConnectionError)
		return
	}
	defer client.Disconnect()

	if !lu.IsDefaultProject() {
		err = client.UseProject(lu.Project)
		if err != nil {
			log.Errorf("sftp: using project %s error: %v", lu.Project, err)
			io.WriteString(s, fmt.Sprintf("unknown project %s\n", lu.Project))
			s.Exit(ExitCodeInvalidProject)
			return
		}
	}

	instance, err := client.GetCachedInstance(lu.Project, lu.Instance)
	if err != nil {
		log.Errorf("sftp: cannot get instance for %s: %s", lu, err)
		io.WriteString(s, fmt.Sprintf("cannot get instance %s\n", lu.FullInstance()))
		s.Exit(ExitCodeMetaError)
		return
	}
	//log.Debugf("sftp: instance: %#v", instance)

	sftpServerBinBytes, err := sftp_server_binary.BinBytes(instance.Architecture)
	if err != nil {
		log.Errorf("sftp: failed to get sftp-server binary: %s", err)
		io.WriteString(s, fmt.Sprintf("failed to get sftp-server binary\n"))
		s.Exit(ExitCodeInternalError)
		return
	}
	sftpServerBinBytes, err = util.Ungz(sftpServerBinBytes)
	if err != nil {
		log.Errorf("sftp: failed to ungzip sftp-server: %s", err)
		io.WriteString(s, fmt.Sprintf("failed to prepare sftp-server\n"))
		s.Exit(ExitCodeInternalError)
		return
	}

	existsParams := &incus.FileExistsParams{
		Project:     lu.Project,
		Instance:    lu.Instance,
		Path:        sftp_server_binary.BinName(),
		Md5sum:      util.Md5Bytes(sftpServerBinBytes),
		ShouldCache: true,
	}
	if !client.FileExists(existsParams) {
		err = client.UploadBytes(lu.Project, lu.Instance, sftp_server_binary.BinName(), bytes.NewReader(sftpServerBinBytes), 0, 0, 0755)
		if err != nil {
			log.Errorf("sftp: upload failed: %v", err)
			io.WriteString(s, fmt.Sprintf("sftp-server is not available on %s\n", lu.FullInstance()))
			s.Exit(ExitCodeConnectionError)
			return
		}
		log.Debugf("sftp: sftp-server uploaded %s to %s", sftp_server_binary.BinName(), lu.FullInstance())
	}
	sftpServerBinBytes = nil

	iu, err := client.GetCachedInstanceUser(lu.Project, lu.Instance, lu.InstanceUser)
	if err != nil {
		log.Errorf("failed to get instance user %s for %s: %s", lu.InstanceUser, lu, err)
		io.WriteString(s, fmt.Sprintf("cannot get instance user %s\n", lu.InstanceUser))
		s.Exit(ExitCodeMetaError)
		return
	}

	if iu == nil {
		io.WriteString(s, "not found user or instance\n")
		log.Errorf("sftp: not found instance user for %s", lu)
		s.Exit(ExitCodeInvalidLogin)
		return
	}

	//log.Debugf("sftp: found instance user %s [%d %d]", iu.User, iu.Uid, iu.Gid)

	stdin, stderr, cleanup := util.SetupPipes(s)
	defer cleanup()

	uid := 0
	gid := 0
	home := iu.Dir
	startDir := home

	cmd := fmt.Sprintf("%s -e", sftp_server_binary.BinName())
	if config.ChrootSFTP && iu.Uid != 0 {
		home = "/"
		cmd += " -c"
	}
	cmd += fmt.Sprintf(" -d %s", startDir)

	env := make(map[string]string)
	env["USER"] = iu.User
	//if iu.Group != "" {
	//	env["GROUP"] = iu.Group
	//}
	env["UID"] = fmt.Sprintf("%d", iu.Uid)
	env["GID"] = fmt.Sprintf("%d", iu.Gid)
	env["HOME"] = home
	env["SSH_SESSION"] = s.Context().ShortSessionID()

	log.Debugf("sftp: CMD %s", cmd)
	log.Debugf("sftp: ENV %s", util.MapToEnvString(env))

	ie := client.NewInstanceExec(incus.InstanceExec{
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
	if err != nil && err != io.EOF && !errors.Is(err, context.Canceled) {
		io.WriteString(s, "sftp connection failed\n")
		log.Errorf("sftp: exec failed: %s", err)
	}

	err = s.Exit(ret)
	if err != nil && err != io.EOF {
		log.Errorf("sftp: session exit failed: %v", err)
	}
	log.Debugf("sftp: exit %d", ret)
}
