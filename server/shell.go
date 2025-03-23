package server

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"ssh2incus/pkg/incus"
	"ssh2incus/pkg/util/shlex"
	"ssh2incus/pkg/util/ssh"

	"github.com/creack/pty"
	log "github.com/sirupsen/logrus"
)

func shellHandler(s ssh.Session) {
	lu, ok := s.Context().Value("LoginUser").(LoginUser)
	if !ok || !lu.IsValid() {
		log.Errorf("invalid connection data for %#v", lu)
		io.WriteString(s, "invalid connection data")
		s.Exit(1)
		return
	}
	log.Debugf("shell: connecting %#v", lu)

	if lu.User == "root" && lu.Instance == "%shell" {
		incusShell(s)
		return
	}

	params, err := GetServerParams()
	if err != nil {
		log.Errorf("failed to get Incus connection parameters: %w", err)
		s.Exit(255)
		return
	}
	server, err := incus.Connect(s.Context(), params)
	if err != nil {
		log.Errorln(err.Error())
		s.Exit(255)
		return
	}
	defer server.Disconnect()

	if !lu.IsDefaultProject() {
		server, err = incus.UseProject(server, lu.Project)
		if err != nil {
			log.Errorf("using project %s error: %s", lu.Project, err)
			io.WriteString(s, fmt.Sprintf("unknown project %s\n", lu.Project))
			s.Exit(2)
			return
		}
	}

	var iu *incus.InstanceUser
	if lu.InstanceUser != "" {
		iu = incus.GetInstanceUser(server, lu.Instance, lu.InstanceUser)
	}

	if iu == nil {
		io.WriteString(s, "not found user or instance\n")
		log.Errorf("shell: not found instance user for %#v", lu)
		s.Exit(1)
		return
	}

	env := make(map[string]string)
	for _, v := range s.Environ() {
		k := strings.SplitN(v, "=", 2)
		env[k[0]] = k[1]
	}

	if ssh.AgentRequested(s) {
		l, err := ssh.NewAgentListener()
		if err != nil {
			log.Errorln(err.Error())
		} else {
			defer l.Close()
			go ssh.ForwardAgentConnections(l, s)

			d := &incus.ProxyDevice{
				Server:   &server,
				Project:  lu.Project,
				Instance: lu.Instance,
				Source:   l.Addr().String(),
				Uid:      iu.Uid,
				Gid:      iu.Gid,
				Mode:     "0660",
			}

			if socket, err := d.AddSocket(); err == nil {
				env["SSH_AUTH_SOCK"] = socket
				defer d.RemoveSocket()
			} else {
				log.Errorln(err.Error())
			}
		}
	}

	ptyReq, winCh, isPty := s.Pty()

	if ptyReq.Term != "" {
		env["TERM"] = ptyReq.Term
	} else {
		env["TERM"] = "xterm-256color"
	}

	env["USER"] = iu.User
	env["HOME"] = iu.Dir
	env["SHELL"] = iu.Shell

	var cmd string
	var shouldRunAsUser bool
	if s.RawCommand() == "" {
		switch config.Shell {
		case ShellSu:
			cmd = fmt.Sprintf(`su - "%s"`,
				iu.User,
			)
		case ShellLogin:
			cmd = fmt.Sprintf(`login -h "%s" -f "%s"`,
				strings.Split(s.RemoteAddr().String(), ":")[0],
				iu.User,
			)
		default:
			shouldRunAsUser = true
			cmd = fmt.Sprintf("%s -l", iu.Shell)
		}
	} else {
		shouldRunAsUser = true
		cmd = s.RawCommand()
		if strings.Contains(cmd, "$") {
			cmd = fmt.Sprintf(`%s -c "%s"`, iu.Shell, cmd)
		}
	}

	log.Debugf("shell cmd: %v", cmd)
	log.Debugf("shell pty: %v", isPty)
	log.Debugf("shell env: %v", env)

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

	windowChannel := make(incus.WindowChannel)
	go func() {
		for win := range winCh {
			windowChannel <- incus.Window{Width: win.Width, Height: win.Height}
		}
	}()

	var uid, gid int
	if shouldRunAsUser {
		uid, gid = iu.Uid, iu.Gid
	}

	ie := &incus.InstanceExec{
		Server:   &server,
		Instance: lu.Instance,
		Cmd:      cmd,
		Env:      env,
		IsPty:    isPty,
		Window:   incus.Window(ptyReq.Window),
		WinCh:    windowChannel,
		Stdin:    stdin,
		Stdout:   s,
		Stderr:   stderr,
		User:     uid,
		Group:    gid,
		Cwd:      iu.Dir,
	}

	ret, err := ie.Exec()
	if err != nil {
		log.Debugln("shell: connection failed")
	}

	s.Exit(ret)
}

func incusShell(s ssh.Session) {
	cmdString := `bash -c 'while true; do read -r -p "
Type incus command:
> incus " a; incus $a; done'`

	args, _ := shlex.Split(cmdString, true)
	cmd := exec.Command(args[0], args[1:]...)

	ptyReq, winCh, isPty := s.Pty()
	if isPty {
		cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
		cmd.Env = append(cmd.Env, "PATH=/bin:/usr/bin:/snap/bin:/usr/local/bin")
		cmd.Env = append(cmd.Env, fmt.Sprintf("INCUS_SOCKET=%s", config.IncusSocket))
		f, err := pty.Start(cmd)
		if err != nil {
			log.Errorln(err.Error())
			io.WriteString(s, "Couldn't allocate PTY\n")
			s.Exit(-1)
		}
		io.WriteString(s, `
incus shell emulator. Use Ctrl+c to exit

Hit Enter or type 'help' for help
`)
		go func() {
			for win := range winCh {
				setWinsize(f, win.Width, win.Height)
			}
		}()
		go func() {
			io.Copy(f, s) // stdin
		}()
		io.Copy(s, f) // stdout
		cmd.Wait()
	} else {
		io.WriteString(s, "No PTY requested.\n")
		s.Exit(1)
	}
}

func setWinsize(f *os.File, w, h int) {
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&struct{ h, w, x, y uint16 }{uint16(h), uint16(w), 0, 0})))
}
