package server

import (
	"bufio"
	"context"
	"errors"
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

// Constants for exit codes
const (
	ExitCodeNotImplemented    = -1
	ExitCodeInvalidLogin      = 1
	ExitCodeInvalidProject    = 2
	ExitCodeMetaError         = 3
	ExitCodeArchitectureError = 4
	ExitCodeInternalError     = 20
	ExitCodeConnectionError   = 255
)

// setupEnvironmentVariables creates and populates the environment map
func setupEnvironmentVariables(s ssh.Session, iu *incus.InstanceUser, ptyReq ssh.Pty) map[string]string {
	env := make(map[string]string)

	// Parse environment variables from session
	for _, v := range s.Environ() {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}

	// Set terminal info
	if ptyReq.Term != "" {
		env["TERM"] = ptyReq.Term
	} else {
		env["TERM"] = "xterm-256color"
	}

	// Set user info
	env["USER"] = iu.User
	env["HOME"] = iu.Dir
	env["SHELL"] = iu.Shell

	return env
}

// buildCommandString creates the appropriate command string based on configuration
func buildCommandString(s ssh.Session, iu *incus.InstanceUser, remoteAddr string) (string, bool) {
	var cmd string
	var shouldRunAsUser bool

	if s.RawCommand() == "" {
		switch config.Shell {
		case ShellSu:
			cmd = fmt.Sprintf(`su - "%s"`, iu.User)
		case ShellLogin:
			host := strings.Split(remoteAddr, ":")[0]
			cmd = fmt.Sprintf(`login -h "%s" -f "%s"`, host, iu.User)
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

	return cmd, shouldRunAsUser
}

func shellHandler(s ssh.Session) {
	lu, ok := s.Context().Value("LoginUser").(LoginUser)
	if !ok || !lu.IsValid() {
		log.Errorf("invalid connection data for %#v", lu)
		io.WriteString(s, "invalid connection data")
		s.Exit(ExitCodeInvalidLogin)
		return
	}
	log.Debugf("shell: connecting %#v", lu)

	if lu.User == "root" && lu.Instance == "%shell" {
		incusShell(s)
		return
	}

	server, err := NewIncusServer()
	if err != nil {
		log.Errorf("failed to initialize incus client: %w", err)
		s.Exit(ExitCodeConnectionError)
		return
	}

	err = server.Connect(s.Context())
	if err != nil {
		log.Errorf("failed to connect to incus: %w", err)
		s.Exit(ExitCodeConnectionError)
		return
	}
	defer server.Disconnect()

	// Project handling
	if !lu.IsDefaultProject() {
		err = server.UseProject(lu.Project)
		if err != nil {
			log.Errorf("using project %s error: %w", lu.Project, err)
			io.WriteString(s, fmt.Sprintf("unknown project %s\n", lu.Project))
			s.Exit(ExitCodeInvalidProject)
			return
		}
	}

	// User handling
	var iu *incus.InstanceUser
	if lu.InstanceUser != "" {
		iu = server.GetInstanceUser(lu.Project, lu.Instance, lu.InstanceUser)
	}

	if iu == nil {
		io.WriteString(s, "not found user or instance\n")
		log.Errorf("shell: not found instance user for %#v", lu)
		s.Exit(ExitCodeInvalidLogin)
		return
	}

	// Get PTY information
	ptyReq, winCh, isPty := s.Pty()

	// Setup environment
	env := setupEnvironmentVariables(s, iu, ptyReq)

	// Setup SSH agent if requested
	if ssh.AgentRequested(s) {
		l, err := ssh.NewAgentListener()
		if err != nil {
			log.Errorf("Failed to create agent listener: %w", err)
			return
		}

		defer l.Close()
		go ssh.ForwardAgentConnections(l, s)

		d := server.NewProxyDevice(incus.ProxyDevice{
			Project:  lu.Project,
			Instance: lu.Instance,
			Source:   l.Addr().String(),
			Uid:      iu.Uid,
			Gid:      iu.Gid,
			Mode:     "0660",
		})

		if socket, err := d.AddSocket(); err == nil {
			env["SSH_AUTH_SOCK"] = socket
			defer d.RemoveSocket()
		} else {
			log.Errorf("Failed to add socket: %w", err)
		}
	}

	// Build command string
	cmd, shouldRunAsUser := buildCommandString(s, iu, s.RemoteAddr().String())

	log.Debugf("shell cmd: %v", cmd)
	log.Debugf("shell pty: %v", isPty)
	log.Debugf("shell env: %v", env)

	// Setup I/O pipes
	stdin, _, stderr := setupShellPipes(s)

	// Setup window size channel
	windowChannel := make(incus.WindowChannel)
	defer close(windowChannel)

	go func() {
		for win := range winCh {
			windowChannel <- incus.Window{Width: win.Width, Height: win.Height}
		}
	}()

	var uid, gid int
	if shouldRunAsUser {
		uid, gid = iu.Uid, iu.Gid
	}

	ie := server.NewInstanceExec(incus.InstanceExec{
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
	})

	ret, err := ie.Exec()
	if err != nil {
		log.Debugf("shell: connection failed: %w", err)
	}

	s.Exit(ret)
}

func incusShell(s ssh.Session) {
	cmdString := `bash -c 'while true; do read -r -p "
Type incus command:
> incus " a; incus $a; done'`

	args, err := shlex.Split(cmdString, true)
	if err != nil {
		log.Errorf("command parsing failed: %w", err)
		io.WriteString(s, "Internal error: command parsing failed\n")
		s.Exit(ExitCodeConnectionError)
		return
	}

	cmd := exec.Command(args[0], args[1:]...)

	ptyReq, winCh, isPty := s.Pty()
	if !isPty {
		io.WriteString(s, "No PTY requested.\n")
		s.Exit(ExitCodeConnectionError)
		return
	}

	cmd.Env = append(cmd.Env,
		fmt.Sprintf("TERM=%s", ptyReq.Term),
		"PATH=/bin:/usr/bin:/snap/bin:/usr/local/bin",
		fmt.Sprintf("INCUS_SOCKET=%s", config.IncusSocket),
	)

	f, err := pty.Start(cmd)
	if err != nil {
		log.Errorf("pty start failed: %w", err)
		io.WriteString(s, "Couldn't allocate PTY\n")
		s.Exit(ExitCodeConnectionError)
		return
	}
	defer f.Close()

	// Welcome message
	io.WriteString(s, `
incus shell emulator. Use Ctrl+c to exit

Hit Enter or type 'help' for help
`)
	// Create a context that will be canceled when the function exits
	ctx, cancel := context.WithCancel(s.Context())
	defer cancel()

	// Handle window resize events
	go func() {
		for {
			select {
			case win, ok := <-winCh:
				if !ok {
					return
				}
				setWinsize(f, win.Width, win.Height)
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		defer cancel()
		bufIn := bufio.NewReader(s)
		_, err := io.Copy(f, bufIn)
		if err != nil && !errors.Is(err, io.EOF) {
			log.Debugf("stdin copy error: %w", err)
		}
	}()

	bufOut := bufio.NewWriter(s)
	_, err = io.Copy(bufOut, f)
	if err != nil && !errors.Is(err, io.EOF) {
		log.Debugf("stdout copy error: %w", err)
	}

	// Wait for the command to finish and check for errors
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			s.Exit(exitErr.ExitCode())
		} else {
			log.Errorf("command wait error: %w", err)
			s.Exit(ExitCodeConnectionError)
		}
	}
}

// Helper function to setup stdin/stdout/stderr pipes
func setupShellPipes(s ssh.Session) (io.ReadCloser, io.Writer, io.WriteCloser) {
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

	return stdin, s, stderr
}

func setWinsize(f *os.File, w, h int) {
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&struct{ h, w, x, y uint16 }{uint16(h), uint16(w), 0, 0})))
}
