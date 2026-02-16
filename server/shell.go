package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"ssh2incus/pkg/incus"
	"ssh2incus/pkg/shlex"
	"ssh2incus/pkg/ssh"
	"ssh2incus/pkg/util"
	"ssh2incus/pkg/util/buffer"

	"github.com/creack/pty"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
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

const (
	ShellSu    = "su"
	ShellSush  = "sush"
	ShellLogin = "login"
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

	if _, ok := env["TERM"]; !ok {
		if ptyReq.Term != "" {
			env["TERM"] = ptyReq.Term
		} else {
			env["TERM"] = "xterm-256color"
		}
	}

	// Set user info
	env["USER"] = iu.User
	env["HOME"] = iu.Dir
	env["SHELL"] = iu.Shell
	env["SSH_SESSION"] = s.Context().ShortSessionID()

	return env
}

// buildCommandString creates the appropriate command string based on configuration
func buildCommandString(s ssh.Session, iu *incus.InstanceUser, remoteAddr string) (string, bool) {
	var cmd string
	var shouldRunAsUser bool

	if s.RawCommand() == "" {
		switch config.Shell {
		case ShellSu:
			cmd = fmt.Sprintf("su - %q", iu.User)
		case ShellSush:
			cmd = fmt.Sprintf("sush %q", iu.User)
		case ShellLogin:
			h := ""
			host, _, err := net.SplitHostPort(remoteAddr)
			if err == nil {
				h = fmt.Sprintf("-h %q ", host)
			}
			cmd = fmt.Sprintf("login %s-f %q", h, iu.User)
		default:
			shouldRunAsUser = true
			cmd = fmt.Sprintf("%s -l", iu.Shell)
		}
	} else {
		shouldRunAsUser = true
		cmd = s.RawCommand()
		if needsShellWrapping(cmd) {
			cmd = fmt.Sprintf("%s -c %q", iu.Shell, cmd)
		}
	}

	return cmd, shouldRunAsUser
}

// TermMuxWriter interface allows sending messages to different connection types (SSH, WebSocket, etc.)
type TermMuxWriter interface {
	Write(p []byte) (n int, err error)
}

func checkTermMux(w TermMuxWriter, tmux *TermMux, c *incus.Client, lu *LoginUser, iu *incus.InstanceUser, env map[string]string) error {
	existsParams := &incus.CommandExistsParams{
		Project:     lu.Project,
		Instance:    lu.Instance,
		Path:        tmux.Name(),
		ShouldCache: false,
	}
	if !c.CommandExists(existsParams) {
		log.Debugf("command not found: %s", tmux.Name())
		// DISABLED until fully tested
		// use built-in static binary for tmux
		//if config.TermMux == "tmux" {
		//	client, err := NewDefaultIncusClientWithContext(tmux.ctx)
		//	if err != nil {
		//		return err
		//	}
		//	defer client.Disconnect()
		//
		//	if !lu.IsDefaultProject() {
		//		err = client.UseProject(lu.Project)
		//		if err != nil {
		//			return err
		//		}
		//	}
		//	instance, err := client.GetCachedInstance(lu.Project, lu.Instance)
		//	if err != nil {
		//		return fmt.Errorf("cannot get instance for %s: %v", lu, err)
		//	}
		//	tmuxBinBytes, err := tmux_binary.BinBytes(instance.Architecture)
		//	if err != nil {
		//		return fmt.Errorf("failed to get tmux binary for %s: %v", instance.Architecture, err)
		//	}
		//	tmuxBinBytes, err = util.Ungz(tmuxBinBytes)
		//	if err != nil {
		//		return fmt.Errorf("failed to ungzip tmux: %v", err)
		//	}
		//	err = client.UploadBytes(lu.Project, lu.Instance, tmux_binary.BinName(), bytes.NewReader(tmuxBinBytes), 0, 0, 0755)
		//	if err != nil {
		//		return fmt.Errorf("upload failed: %v", err)
		//	}
		//
		//	terminfoPath := "etc/terminfo/x/xterm-256color"
		//	terminfoBytes, err := tmux_binary.TerminfoFS().ReadFile(terminfoPath)
		//	if err != nil {
		//		return fmt.Errorf("failed to get terminfo library %s: %v", terminfoPath, err)
		//	}
		//	err = client.UploadBytes(lu.Project, lu.Instance, "/"+terminfoPath, bytes.NewReader(terminfoBytes), 0, 0, 0755)
		//	if err != nil {
		//		return fmt.Errorf("upload failed: %v", err)
		//	}
		//
		//} else {

		fmt.Fprintf(w, "\r\ninstalling %s...\r\n\n", tmux.Name())
		err := c.InstallPackages(lu.Project, lu.Instance, []string{tmux.Name()})
		if err != nil {
			return err
		}
		//}
	}

	uid, gid := iu.Uid, iu.Gid
	stdout := buffer.NewOutputBuffer()
	stderr := buffer.NewOutputBuffer()
	ie := c.NewInstanceExec(incus.InstanceExec{
		Instance: lu.Instance,
		Cmd:      tmux.List(),
		Stdout:   stdout,
		Stderr:   stderr,
		User:     uid,
		Group:    gid,
	})
	ret, err := ie.Exec()
	if err != nil && err != io.EOF {
		log.Errorf("%s: list failed: %v", tmux.Name(), err)
	}

	if !tmux.SessionExists(stdout.Lines()) {
		ie = c.NewInstanceExec(incus.InstanceExec{
			Instance: lu.Instance,
			Cmd:      tmux.New(),
			Env:      env,
			User:     uid,
			Group:    gid,
			Cwd:      iu.Dir,
		})
		ret, err = ie.Exec()
		if err != nil && err != io.EOF {
			log.Errorf("%s: new session failed: %v", tmux.Name(), err)
		}

		if ret != 0 {
			return fmt.Errorf("%s: new session failed with non-zero exit code: %d", tmux.Name(), ret)
		}
	}

	return nil
}

func shellHandler(s ssh.Session) {
	log := log.WithField("session", s.Context().ShortSessionID())

	lu := LoginUserFromContext(s.Context())
	if !lu.IsValid() {
		log.Warnf("invalid login for %s", lu)
		fmt.Fprintf(s, "invalid login for %q (%s)\r\n", lu.OrigUser, lu)
		s.Exit(ExitCodeInvalidLogin)
		return
	}

	// Only root is allowed to access Incus shell
	if lu.User == "root" {
		switch lu.Command {
		case "shell":
			incusShell(s)
			return
		case "remove":
			removeHandler(s)
			return
		}
	}

	// Handle explain command
	if lu.Command == "explain" {
		explainHandler(s)
		return
	}

	if lu.IsCommand() {
		log.Warnf("shell: command %q not allowed", lu)
		fmt.Fprintf(s, "\r\n/%s not allowed\r\n", lu.Command)
		s.Exit(ExitCodeInvalidLogin)
		return
	}

	client, err := NewDefaultIncusClientWithContext(s.Context())
	if err != nil {
		log.Error(err)
		s.Exit(ExitCodeConnectionError)
		return
	}
	defer client.Disconnect()

	var iu *incus.InstanceUser
	if lu.CreateInstance {
		if !config.AllowCreate {
			fmt.Fprint(s, "\r\nInstance creation not allowed\r\n")
			log.Debugf("shell: instance creation not allowed %s", lu)
			s.Exit(ExitCodeInvalidLogin)
			return
		} else {
			iu, _ = client.GetInstanceUser(lu.Project, lu.Instance, lu.InstanceUser)

			// Only attempt to create an instance if it doesn't exist
			if iu == nil {
				log.Debugf("shell: creating instance %s", lu)
				fmt.Fprint(s, "creating instance...\r\n")

				home, _ := os.UserHomeDir()
				cc, err := LoadCreateConfigWithFallback(
					[]string{
						"",
						path.Join(home, ".config", config.App.Name()),
						path.Join("/etc", config.App.Name()),
					})
				if err != nil {
					log.Error(err)
				}
				err = cc.ApplyProfiles(lu.CreateConfig.Profiles)
				if err != nil {
					log.Errorf("shell: create config: %v", err)
					fmt.Fprintf(s, "%s\r\n", err)
					s.Exit(ExitCodeConnectionError)
					return
				}

				params := incus.CreateInstanceParams{
					Name:      lu.Instance,
					Project:   lu.Project,
					Image:     cc.Image(),
					Memory:    cc.Memory(),
					CPU:       cc.CPU(),
					Disk:      cc.Disk(),
					Ephemeral: cc.Ephemeral(),
					VM:        cc.VM(),
					Config:    cc.Config(),
					Devices:   cc.Devices(),
				}

				if lu.CreateConfig.Image != nil {
					params.Image = *lu.CreateConfig.Image
				}

				if lu.CreateConfig.Memory != nil {
					params.Memory = *lu.CreateConfig.Memory
				}

				if lu.CreateConfig.CPU != nil {
					params.CPU = *lu.CreateConfig.CPU
				}

				if lu.CreateConfig.Disk != nil {
					params.Disk = *lu.CreateConfig.Disk
				}

				if lu.CreateConfig.Ephemeral != nil {
					params.Ephemeral = *lu.CreateConfig.Ephemeral
				}

				if lu.CreateConfig.Nesting != nil {
					params.Nesting = *lu.CreateConfig.Nesting
				}

				if lu.CreateConfig.Privileged != nil {
					params.Privileged = *lu.CreateConfig.Privileged
				}

				if lu.CreateConfig.VM != nil {
					params.VM = *lu.CreateConfig.VM
				}

				if params.Ephemeral {
					fmt.Fprint(s, "\r\ntip: run `sudo poweroff` to destroy ephemeral instance\r\n")
				}

				log.Debugf("shell: create instance config: %+v", params)

				_, err = client.CreateInstance(params)
				if err != nil {
					log.Warnf("shell: failed to create instance %s: %v", lu, err)
					fmt.Fprintf(s, "\r\ncannot create instance:\r\n%s\r\n", err)
					s.Exit(ExitCodeInternalError)
					return
				}
				// try this if instance user is not root assuming that it needs to be created by cloud-init
				if lu.InstanceUser != "root" {
					for i := range 20 {
						sleep := time.Duration(i/10+1) * time.Second
						time.Sleep(sleep)
						iu, err := client.GetInstanceUser(params.Project, params.Name, lu.InstanceUser)
						if iu != nil && err == nil {
							break
						}
					}
				}
			}
		}
	}

	log.Debugf("shell: connecting %s", lu)

	if iu == nil {
		iu, err = client.GetCachedInstanceUser(lu.Project, lu.Instance, lu.InstanceUser)
		if err != nil {
			log.Errorf("shell: failed to get instance user %q for %s: %s", lu.InstanceUser, lu, err)
			fmt.Fprintf(s, "\r\ncannot get instance user %q\r\n", lu.InstanceUser)
			s.Exit(ExitCodeInvalidLogin)
			return
		}
	}

	if iu == nil {
		log.Errorf("shell: not found instance user for %q", lu)
		fmt.Fprintf(s, "\r\nnot found user or instance for %q\r\n", lu)
		s.Exit(ExitCodeInvalidLogin)
		return
	}

	// Get PTY information
	ptyReq, sshWinCh, isPty := s.Pty()
	isRaw := s.RawCommand() != ""

	// Setup environment
	env := setupEnvironmentVariables(s, iu, ptyReq)

	// Setup SSH agent if requested
	if ssh.AgentRequested(s) {
		al, err := ssh.NewAgentListener()
		if err != nil {
			log.Errorf("shell: failed to create agent listener: %v", err)
			fmt.Fprintf(s.Stderr(), "\r\nfailed to setup agent\r\n")
		}

		defer al.Close()
		go ssh.ForwardAgentConnections(al, s)

		pd := client.NewProxyDevice(incus.ProxyDevice{
			Project:  lu.Project,
			Instance: lu.Instance,
			Source:   al.Addr().String(),
			Uid:      iu.Uid,
			Gid:      iu.Gid,
			Mode:     "0660",
		})

		if socket, err := pd.AddSocket(); err == nil {
			env["SSH_AUTH_SOCK"] = socket
			deviceRegistry.AddDevice(pd)
			defer func() {
				go pd.Shutdown()
			}()
		} else {
			log.Errorf("shell: failed to add agent socket: %v", err)
			fmt.Fprintf(s.Stderr(), "\r\nfailed to setup agent socket\r\n")
		}
	}

	// Build command string
	cmd, shouldRunAsUser := buildCommandString(s, iu, s.RemoteAddr().String())

	if lu.Persistent {
		usePrefix := false
		//if config.TermMux == "tmux" {
		//	usePrefix = true
		//  env["TERM"] = "xterm-256color"
		//}
		tmux, err := NewTermMux(s.Context(), config.TermMux, config.App.Name(), usePrefix)
		if err != nil {
			log.Errorf("shell: failed to initialize terminal mux: %v", err)
			fmt.Fprintf(s.Stderr(), "\r\nfailed to create persistent session\r\n")
		}
		err = checkTermMux(s, tmux, client, lu, iu, env)
		if err != nil {
			log.Errorf("shell: failed to create persistent session: %v", err)
			fmt.Fprintf(s.Stderr(), "\r\nfailed to create persistent session:\r\n%s\r\n", err)
		}

		shouldRunAsUser = true
		cmd = tmux.Attach()
	}

	log.Debugf("shell: CMD %s", oneLine(cmd))
	log.Debugf("shell: PTY %v", isPty)
	log.Debugf("shell: ENV %s", oneLine(util.MapToEnvString(env)))

	if welcome := welcomeHandler(iu); config.Welcome && isPty && !isRaw && welcome != "" {
		fmt.Fprintf(s, "\r\n%s\r\n\n", welcome)
	}

	// Setup I/O pipes
	stdin, stderr := setupShellPipesWithCleanup(s)
	defer func() {
		stdin.Close()
		stderr.Close()
	}()

	//Setup window size channel
	incusWinCh := make(incus.WindowChannel)
	defer close(incusWinCh)
	startWindowChannel(sshWinCh, incusWinCh)

	var uid, gid int
	if shouldRunAsUser {
		uid, gid = iu.Uid, iu.Gid
	}

	ie := client.NewInstanceExec(incus.InstanceExec{
		Instance: lu.Instance,
		Cmd:      cmd,
		Env:      env,
		IsPty:    isPty,
		Window:   incus.Window(ptyReq.Window),
		WinCh:    incusWinCh,
		Stdin:    stdin,
		Stdout:   s,
		Stderr:   stderr,
		User:     uid,
		Group:    gid,
		Cwd:      iu.Dir,
	})

	ret, err := ie.Exec()
	if err != nil && err != io.EOF && !errors.Is(err, context.Canceled) {
		log.Errorf("shell: exec failed: %v", err)
	}

	err = s.Exit(ret)
	if err != nil && err != io.EOF {
		log.Errorf("shell: failed to exit ssh session: %v", err)
	}
	log.Debugf("shell: exit %d", ret)
}

func incusShell(s ssh.Session) {
	log := log.WithField("session", s.Context().ShortSessionID())

	cmdString := `/bin/bash -c 'while true; do read -r -p "
Type incus command:
> incus " a; incus $a; done'`

	args, err := shlex.Split(cmdString, true)
	if err != nil {
		log.Errorf("command parsing failed: %v", err)
		fmt.Fprintf(s, "Internal error: command parsing failed\r\n")
		s.Exit(ExitCodeConnectionError)
		return
	}

	cmd := exec.Command(args[0], args[1:]...)

	ptyReq, winCh, isPty := s.Pty()
	if !isPty {
		fmt.Fprint(s, "No PTY requested\r\n")
		s.Exit(ExitCodeConnectionError)
		return
	}

	if ptyReq.Term != "" {
		cmd.Env = append(cmd.Env,
			fmt.Sprintf("TERM=%s", ptyReq.Term))
	}

	cmd.Env = append(cmd.Env,
		fmt.Sprintf("SSH_SESSION=%s", s.Context().ShortSessionID()),
		"PATH=/bin:/usr/bin:/snap/bin:/usr/local/bin",
	)

	if config.IncusSocket != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("INCUS_SOCKET=%s", config.IncusSocket))
	}

	log.Debugf("incus shell: CMD %s", oneLine(strings.Join(cmd.Args, " ")))
	log.Debugf("incus shell: PTY %v", isPty)
	log.Debugf("incus shell: ENV %s", oneLine(strings.Join(cmd.Env, " ")))

	p, err := pty.Start(cmd)
	if err != nil {
		log.Errorf("incus shell: %v", err)
		fmt.Fprintf(s, "Could not allocate PTY\r\n")
		s.Exit(-1)
	}
	defer p.Close()

	hostname, _ := os.Hostname()
	fmt.Fprintf(s, `
incus shell emulator on %s (Ctrl+C to exit)

Hit ENTER or type 'help <command>' for help about any command
`, hostname)

	go func() {
		for win := range winCh {
			setWinsize(p, win.Width, win.Height)
		}
	}()

	// Create a context with cancel function to coordinate goroutines
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle stdin
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := s.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Errorf("incus shell: stdin read error: %v", err)
				}
				cancel() // Signal other goroutines to exit
				return
			}

			// Check for Ctrl+C (ASCII value 3)
			for i := range n {
				if buf[i] == 3 {
					log.Debugf("incus shell: received Ctrl+C, exiting")
					fmt.Fprint(s, "\r\nExiting incus shell\r\n")
					cancel() // Signal other goroutines to exit
					return
				}
			}

			// Write the data to the PTY if we're still running
			select {
			case <-ctx.Done():
				return
			default:
				if _, err := p.Write(buf[:n]); err != nil {
					log.Errorf("incus shell: pty write error: %v", err)
					cancel()
					return
				}
			}
		}
	}()

	// Handle stdout
	go func() {
		buf := make([]byte, 32*1024)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, err := p.Read(buf)
				if err != nil {
					if err != io.EOF {
						log.Errorf("incus shell: stdout read error: %v", err)
					}
					cancel()
					return
				}

				if _, err := s.Write(buf[:n]); err != nil {
					log.Errorf("incus shell: session write error: %v", err)
					cancel()
					return
				}
			}
		}
	}()

	// Wait for context to be canceled (Ctrl+C or error)
	<-ctx.Done()

	// Clean exit without waiting for the command
	s.Exit(0)
}

// TODO remove once new function has been checked
func incusShellLegacy(s ssh.Session) {
	cmdString := `/bin/bash -c 'while true; do read -r -p "
Type incus command:
> incus " a; incus $a; done'`

	args, err := shlex.Split(cmdString, true)
	if err != nil {
		log.Errorf("command parsing failed: %v", err)
		fmt.Fprint(s, "Internal error: command parsing failed\r\n")
		s.Exit(ExitCodeConnectionError)
		return
	}

	cmd := exec.Command(args[0], args[1:]...)

	ptyReq, winCh, isPty := s.Pty()
	if !isPty {
		fmt.Fprint(s, "No PTY requested.\r\n")
		s.Exit(ExitCodeConnectionError)
		return
	}

	cmd.Env = append(cmd.Env,
		fmt.Sprintf("TERM=%s", ptyReq.Term),
		fmt.Sprintf("SSH_SESSION=%s", s.Context().ShortSessionID()),
		"PATH=/bin:/usr/bin:/snap/bin:/usr/local/bin",
	)

	if config.IncusSocket != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("INCUS_SOCKET=%s", config.IncusSocket))
	}

	log.Debugf("incus shell: CMD %s", strings.Join(cmd.Args, " "))
	log.Debugf("incus shell: PTY %v", isPty)
	log.Debugf("incus shell: ENV %s", strings.Join(cmd.Env, " "))

	p, err := pty.Start(cmd)
	if err != nil {
		log.Errorf("incus shell: %v", err)
		fmt.Fprint(s, "Could not allocate PTY\r\n")
		s.Exit(-1)
	}
	defer p.Close()

	fmt.Fprint(s, `
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

func oneLine(lines string) string {
	return strings.ReplaceAll(lines, "\n", "\\n")
}

func needsShellWrapping(cmd string) bool {
	// Empty string doesn't need wrapping
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}

	// Match common shell constructs
	shellPatterns := []*regexp.Regexp{
		regexp.MustCompile(`[|&;()<>\s]`),                     // Shell metacharacters
		regexp.MustCompile(`[*?[\]{}~]`),                      // Glob characters
		regexp.MustCompile(`\$(?:\w+|{.+?}|\(.+?\)|\(.+?\))`), // Variable references and substitutions
		regexp.MustCompile("`[^`]+`"),                         // Backtick command substitution
		regexp.MustCompile(`\b(?:if|for|while|until|case|function|source|alias|export|set|unset)\b`), // Shell keywords
	}

	for _, pattern := range shellPatterns {
		if pattern.MatchString(cmd) {
			return true
		}
	}

	// Check for redirect operators
	redirectOps := []string{" > ", " >> ", " < ", " << ", " 2> ", " 2>> "}
	for _, op := range redirectOps {
		if strings.Contains(cmd, op) {
			return true
		}
	}

	// Check for multiple commands
	if strings.Contains(cmd, " && ") || strings.Contains(cmd, " || ") || strings.Contains(cmd, " ; ") {
		return true
	}

	return false
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
				if err != io.EOF && !isClosedPipe(err) {
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

func startWindowChannel(inputCh <-chan ssh.Window, outputCh chan<- incus.Window) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Debugf("shell: recovered from panic: %v", r)
			}
		}()

		for win := range inputCh {
			select {
			case outputCh <- incus.Window{Width: win.Width, Height: win.Height}:
			default:
				log.Debug("shell: cannot send window size to channel, channel may be closed")
			}
		}
	}()
}

func setWinsize(f *os.File, w, h int) {
	ws := &unix.Winsize{
		Row: uint16(h),
		Col: uint16(w),
	}

	if err := unix.IoctlSetWinsize(int(f.Fd()), unix.TIOCSWINSZ, ws); err != nil {
		log.Debugf("shell: failed to set pty winsize: %v", err)
	}
}

// Helper function to check if an error is a timeout
func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}

// Helper function to check if an error is a timeout
func isClosedPipe(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, io.ErrClosedPipe)
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

func explainHandler(s ssh.Session) {
	lu := LoginUserFromContext(s.Context())
	if lu.ExplainUser == nil {
		fmt.Fprint(s, "Invalid explain command\r\n")
		s.Exit(ExitCodeInvalidLogin)
		return
	}

	eu := lu.ExplainUser
	var o strings.Builder

	o.WriteString("\r\n=== Login String Explanation ===\r\n\n")

	if eu.Remote != "" {
		fmt.Fprintf(&o, "Remote:          %s\r\n", eu.Remote)
	}

	fmt.Fprintf(&o, "Host User:       %s\r\n", eu.User)
	fmt.Fprintf(&o, "Instance:        %s\r\n", eu.Instance)
	fmt.Fprintf(&o, "Project:         %s\r\n", eu.Project)
	fmt.Fprintf(&o, "Instance User:   %s\r\n", eu.InstanceUser)

	if eu.Persistent {
		fmt.Fprint(&o, "Persistent:      true (session persists across connections)\n")
	}

	if eu.CreateInstance {
		fmt.Fprint(&o, "Create Instance: true\r\n")
		if eu.CreateConfig.Image != nil {
			fmt.Fprintf(&o, "  Image:         %s\r\n", *eu.CreateConfig.Image)
		}
		if eu.CreateConfig.Memory != nil {
			fmt.Fprintf(&o, "  Memory:        %d MB\r\n", *eu.CreateConfig.Memory)
		}
		if eu.CreateConfig.CPU != nil {
			fmt.Fprintf(&o, "  CPU:           %d cores\r\n", *eu.CreateConfig.CPU)
		}
		if eu.CreateConfig.Disk != nil {
			fmt.Fprintf(&o, "  Disk:          %d GB\r\n", *eu.CreateConfig.Disk)
		}
		if eu.CreateConfig.Ephemeral != nil && *eu.CreateConfig.Ephemeral {
			fmt.Fprint(&o, "  Ephemeral:     true (destroyed on poweroff)\r\n")
		}
		if eu.CreateConfig.Nesting != nil && *eu.CreateConfig.Nesting {
			fmt.Fprint(&o, "  Nesting:       true\r\n")
		}
		if eu.CreateConfig.Privileged != nil && *eu.CreateConfig.Privileged {
			fmt.Fprint(&o, "  Privileged:    true\r\n")
		}
		if eu.CreateConfig.VM != nil && *eu.CreateConfig.VM {
			fmt.Fprint(&o, "  VM:            true\r\n")
		}
		if len(eu.CreateConfig.Profiles) > 0 {
			fmt.Fprintf(&o, "  Profiles:      %v\r\n", eu.CreateConfig.Profiles)
		}
	}
	fmt.Fprint(&o, "\r\n")

	// Now print to ssh session
	fmt.Fprint(s, o.String())
	s.Exit(0)
}

func removeHandler(s ssh.Session) {
	log := log.WithField("session", s.Context().ShortSessionID())
	lu := LoginUserFromContext(s.Context())

	// Check if RemoveUser is set
	if lu.RemoveUser == nil {
		fmt.Fprint(s, "Invalid remove command\r\n")
		s.Exit(ExitCodeInvalidLogin)
		return
	}

	// Only root host user is allowed to remove instances
	if lu.User != "root" {
		log.Warnf("remove: non-root user %q attempted to remove instance", lu.User)
		fmt.Fprintf(s, "\r\nPermission denied: only root user can remove instances\r\n")
		s.Exit(ExitCodeInvalidLogin)
		return
	}

	ru := lu.RemoveUser

	// Ensure remote is set
	if ru.Remote == "" {
		ru.Remote = config.Remote
	}

	// Create Incus client
	client, err := NewDefaultIncusClientWithContext(s.Context())
	if err != nil {
		log.Errorf("remove: failed to create incus client: %v", err)
		fmt.Fprintf(s, "\r\nFailed to connect to Incus: %v\r\n", err)
		s.Exit(ExitCodeConnectionError)
		return
	}
	defer client.Disconnect()

	// Check if instance exists
	instance, err := client.GetCachedInstance(ru.Project, ru.Instance)
	if err != nil || instance == nil {
		log.Errorf("remove: instance %s.%s not found: %v", ru.Instance, ru.Project, err)
		fmt.Fprintf(s, "\r\nInstance %s.%s not found\r\n", ru.Instance, ru.Project)
		s.Exit(ExitCodeInvalidLogin)
		return
	}

	// Display instance information
	var o strings.Builder
	o.WriteString("\r\n=== Instance Details ===\r\n\n")
	if ru.Remote != "" {
		fmt.Fprintf(&o, "Remote:          %s\r\n", ru.Remote)
	}
	fmt.Fprintf(&o, "Instance:        %s\r\n", ru.Instance)
	fmt.Fprintf(&o, "Project:         %s\r\n", ru.Project)
	fmt.Fprintf(&o, "Status:          %s\r\n", instance.Status)
	fmt.Fprintf(&o, "Type:            %s\r\n", instance.Type)
	if instance.Ephemeral {
		fmt.Fprint(&o, "Ephemeral:       yes\r\n")
	}
	fmt.Fprint(&o, "\r\n")

	// Print instance info
	fmt.Fprint(s, o.String())

	// If force flag is set, skip confirmation
	if !lu.ForceRemove {
		// Ask for confirmation
		fmt.Fprintf(s, "Are you sure you want to delete instance %s.%s? Type 'yes' to confirm: ", ru.Instance, ru.Project)

		// Read user input
		buf := make([]byte, 1024)
		n, err := s.Read(buf)
		if err != nil {
			log.Errorf("remove: failed to read confirmation: %v", err)
			fmt.Fprint(s, "\r\nCancelled\r\n")
			s.Exit(0)
			return
		}

		response := strings.TrimSpace(string(buf[:n]))
		response = strings.TrimRight(response, "\r\n")

		if strings.ToLower(response) != "yes" {
			fmt.Fprint(s, "\r\nCancelled\r\n")
			log.Infof("remove: user cancelled deletion of %s.%s", ru.Instance, ru.Project)
			s.Exit(0)
			return
		}
	}

	// Stop instance if running
	if instance.Status == "Running" {
		fmt.Fprint(s, "\r\nStopping instance...\r\n")
		stopCtx, stopCancel := context.WithTimeout(s.Context(), 60*time.Second)
		defer stopCancel()

		op, err := client.StopInstance(ru.Project, ru.Instance, false)
		if err != nil {
			log.Errorf("remove: failed to stop instance %s.%s: %v", ru.Instance, ru.Project, err)
			fmt.Fprintf(s, "Failed to stop instance: %v\r\n", err)
			s.Exit(ExitCodeInternalError)
			return
		}

		// Wait for stop operation to complete
		err = op.WaitContext(stopCtx)
		if err != nil {
			log.Errorf("remove: stop instance %s.%s failed: %v", ru.Instance, ru.Project, err)
			fmt.Fprintf(s, "Failed to stop instance: %v\r\n", err)
			s.Exit(ExitCodeInternalError)
			return
		}

		// If ephemeral, stopping automatically deletes it
		// Wait for it to be gone
		if instance.Ephemeral {
			fmt.Fprint(s, "Waiting for ephemeral instance to be deleted...\r\n")

			// Poll until instance no longer exists
			waitCtx, waitCancel := context.WithTimeout(s.Context(), 30*time.Second)
			defer waitCancel()

			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-waitCtx.Done():
					log.Errorf("remove: timeout waiting for ephemeral instance %s.%s to be deleted", ru.Instance, ru.Project)
					fmt.Fprint(s, "Timeout waiting for instance to be deleted\r\n")
					s.Exit(ExitCodeInternalError)
					return
				case <-ticker.C:
					exists, err := client.InstanceExists(ru.Instance, ru.Project)
					if err != nil {
						log.Errorf("remove: error checking instance existence: %v", err)
						continue
					}
					if !exists {
						// Instance is gone, success!
						fmt.Fprintf(s, "\r\nEphemeral instance %s.%s deleted successfully\r\n", ru.Instance, ru.Project)
						log.Infof("remove: successfully deleted ephemeral instance %s.%s", ru.Instance, ru.Project)
						s.Exit(0)
						return
					}
				}
			}
		}
	}

	// Delete the instance (non-ephemeral)
	fmt.Fprint(s, "Deleting instance...\r\n")
	deleteCtx, deleteCancel := context.WithTimeout(s.Context(), 60*time.Second)
	defer deleteCancel()

	deleteOp, err := client.DeleteInstance(ru.Project, ru.Instance)
	if err != nil {
		log.Errorf("remove: failed to delete instance %s.%s: %v", ru.Instance, ru.Project, err)
		fmt.Fprintf(s, "Failed to delete instance: %v\r\n", err)
		s.Exit(ExitCodeInternalError)
		return
	}

	// Wait for delete operation to complete
	err = deleteOp.WaitContext(deleteCtx)
	if err != nil {
		log.Errorf("remove: delete instance %s.%s failed: %v", ru.Instance, ru.Project, err)
		fmt.Fprintf(s, "Failed to delete instance: %v\r\n", err)
		s.Exit(ExitCodeInternalError)
		return
	}

	// Success
	fmt.Fprintf(s, "\r\nInstance %s.%s deleted successfully\r\n", ru.Instance, ru.Project)
	log.Infof("remove: successfully deleted instance %s.%s", ru.Instance, ru.Project)
	s.Exit(0)
}
