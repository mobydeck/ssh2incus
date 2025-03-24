package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
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
		log.Errorf("failed to initialize incus client: %v", err)
		s.Exit(ExitCodeConnectionError)
		return
	}

	err = server.Connect(s.Context())
	if err != nil {
		log.Errorf("failed to connect to incus: %v", err)
		s.Exit(ExitCodeConnectionError)
		return
	}
	defer server.Disconnect()

	// Project handling
	if !lu.IsDefaultProject() {
		err = server.UseProject(lu.Project)
		if err != nil {
			log.Errorf("using project %s error: %v", lu.Project, err)
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
			log.Errorf("Failed to create agent listener: %v", err)
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
			deviceRegistry.AddDevice(d)
			defer d.RemoveSocket()
		} else {
			log.Errorf("Failed to add socket: %v", err)
		}
	}

	// Build command string
	cmd, shouldRunAsUser := buildCommandString(s, iu, s.RemoteAddr().String())

	log.Debugf("shell cmd: %v", cmd)
	log.Debugf("shell pty: %v", isPty)
	log.Debugf("shell env: %v", env)

	// Setup I/O pipes
	stdin, stderr := setupShellPipesWithCleanup(s)
	defer func() {
		stdin.Close()
		stderr.Close()
	}()

	//Setup window size channel
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
		log.Errorf("shell exec failed: %v", err)
	}

	err = s.Exit(ret)
	if err != nil {
		log.Errorf("ssh session exit failed: %v", err)
	}
}

func incusShell(s ssh.Session) {
	cmdString := `bash -c 'while true; do read -r -p "
Type incus command:
> incus " a; incus $a; done'`

	args, err := shlex.Split(cmdString, true)
	if err != nil {
		log.Errorf("command parsing failed: %v", err)
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

	p, err := pty.Start(cmd)
	if err != nil {
		log.Errorln(err.Error())
		io.WriteString(s, "Couldn't allocate PTY\n")
		s.Exit(-1)
	}
	defer p.Close()

	io.WriteString(s, `
incus shell emulator. Use Ctrl+c to exit

Hit Enter or type 'help' for help
`)
	go func() {
		for win := range winCh {
			setWinsize(p, win.Width, win.Height)
		}
	}()
	go func() {
		io.Copy(p, s) // stdin
	}()
	io.Copy(s, p) // stdout
	cmd.Wait()
}

// Helper function to setup stdin/stdout/stderr pipes
func setupShellPipes(s ssh.Session) (io.ReadCloser, io.WriteCloser) {
	stdin, inWrite := io.Pipe()
	errRead, stderr := io.Pipe()

	// Get the session's context to track when it ends
	sessionCtx := s.Context()

	// First goroutine: handle stdin
	go func() {
		defer inWrite.Close() // Ensure we always close the pipe

		// Use fixed-size buffer to limit memory usage
		buf := make([]byte, 32*1024) // 32KB buffer

		for {
			// Check if session has ended
			select {
			case <-sessionCtx.Done():
				log.Debug("SSH session closed, closing stdin pipe")
				return
			default:
				// Continue with read operation
			}

			// Read with timeout to periodically check session status
			nr, err := s.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Debugf("Read error from SSH session: %v", err)
				}
				return
			}

			if nr > 0 {
				// Write to pipe
				_, err = inWrite.Write(buf[0:nr])
				if err != nil {
					log.Debugf("Write error to stdin pipe: %v", err)
					return
				}
			}
		}
	}()

	// Second goroutine: handle stderr
	go func() {
		defer errRead.Close() // Ensure we always close the pipe

		// Use fixed-size buffer to limit memory usage
		buf := make([]byte, 32*1024) // 32KB buffer

		for {
			// Check if session has ended
			select {
			case <-sessionCtx.Done():
				log.Debug("SSH session closed, closing stderr pipe")
				return
			default:
				// Continue with read operation
			}

			// Read from pipe
			nr, err := errRead.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Debugf("Read error from stderr pipe: %v", err)
				}
				return
			}

			if nr > 0 {
				// Write to session stderr
				_, err = s.Stderr().Write(buf[0:nr])
				if err != nil {
					log.Debugf("Write error to SSH session stderr: %v", err)
					return
				}
			}
		}
	}()

	return stdin, stderr
}

func setupShellPipesWithCleanup(s ssh.Session) (io.ReadCloser, io.WriteCloser) {
	// Create a pipe registry to track all pipe-related resources
	type pipeResources struct {
		pipes      []io.Closer
		goroutines sync.WaitGroup
	}

	resources := &pipeResources{
		pipes: make([]io.Closer, 0, 4), // Pre-allocate for expected pipes
	}

	// Create the pipes
	stdin, inWrite := io.Pipe()
	errRead, stderr := io.Pipe()

	// Register all pipes for cleanup
	resources.pipes = append(resources.pipes, stdin, inWrite, errRead, stderr)

	// Create a context with cancellation for coordinating goroutine termination
	ctx, cancel := context.WithCancel(context.Background())

	// Track session termination
	sessionDone := make(chan struct{})
	go func() {
		<-s.Context().Done()
		close(sessionDone)
	}()

	// Clean shutdown function
	cleanup := func() {
		// Cancel the context to signal goroutines
		cancel()

		// Close all pipes to unblock any waiting I/O
		for _, p := range resources.pipes {
			p.Close()
		}

		// Wait for all goroutines to finish with a timeout
		done := make(chan struct{})
		go func() {
			resources.goroutines.Wait()
			close(done)
		}()

		// Wait with timeout to avoid hanging
		select {
		case <-done:
			// All goroutines exited cleanly
		case <-time.After(5 * time.Second):
			log.Warn("Timeout waiting for pipe goroutines to exit")
		}
	}

	// First goroutine: stdin handler
	resources.goroutines.Add(1)
	go func() {
		defer resources.goroutines.Done()
		defer inWrite.Close() // Always close our end of the pipe

		buf := make([]byte, 8*1024)

		for {
			// Check if we should terminate
			select {
			case <-ctx.Done():
				return
			case <-sessionDone:
				return
			default:
				// Continue processing
			}

			// Set a read deadline to avoid blocking forever
			if deadline, ok := s.(interface{ SetReadDeadline(time.Time) error }); ok {
				deadline.SetReadDeadline(time.Now().Add(1 * time.Second))
			}

			nr, err := s.Read(buf)
			if err != nil {
				if err != io.EOF && !isTimeout(err) {
					log.Debugf("stdin read error: %v", err)
				}
				if isTimeout(err) {
					// Just a timeout, continue the loop
					continue
				}
				return
			}

			if nr > 0 {
				_, err := inWrite.Write(buf[:nr])
				if err != nil {
					log.Debugf("stdin write error: %v", err)
					return
				}
			}
		}
	}()

	// Second goroutine: stderr handler
	resources.goroutines.Add(1)
	go func() {
		defer resources.goroutines.Done()
		defer errRead.Close() // Always close our end of the pipe

		buf := make([]byte, 8*1024)

		for {
			// Check if we should terminate
			select {
			case <-ctx.Done():
				return
			case <-sessionDone:
				return
			default:
				// Continue processing
			}

			nr, err := errRead.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Debugf("stderr read error: %v", err)
				}
				return
			}

			if nr > 0 {
				_, err := s.Stderr().Write(buf[:nr])
				if err != nil {
					log.Debugf("stderr write error: %v", err)
					return
				}
			}
		}
	}()

	// Create wrapper objects that trigger cleanup on Close()
	cleanupStdin := &cleanupReadCloser{
		ReadCloser: stdin,
		cleanup:    cleanup,
	}

	cleanupStderr := &cleanupWriteCloser{
		WriteCloser: stderr,
		cleanup:     cleanup,
	}

	return cleanupStdin, cleanupStderr
}

func setWinsize(f *os.File, w, h int) {
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&struct{ h, w, x, y uint16 }{uint16(h), uint16(w), 0, 0})))
}

// Helper function to check if an error is a timeout
func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}

// Wrapper types that ensure cleanup happens on Close()
type cleanupReadCloser struct {
	io.ReadCloser
	cleanup    func()
	cleanedUp  bool
	cleanupMux sync.Mutex
}

func (r *cleanupReadCloser) Close() error {
	r.cleanupMux.Lock()
	defer r.cleanupMux.Unlock()

	if !r.cleanedUp {
		r.cleanup()
		r.cleanedUp = true
	}
	return r.ReadCloser.Close()
}

type cleanupWriteCloser struct {
	io.WriteCloser
	cleanup    func()
	cleanedUp  bool
	cleanupMux sync.Mutex
}

func (w *cleanupWriteCloser) Close() error {
	w.cleanupMux.Lock()
	defer w.cleanupMux.Unlock()

	if !w.cleanedUp {
		w.cleanup()
		w.cleanedUp = true
	}
	return w.WriteCloser.Close()
}
