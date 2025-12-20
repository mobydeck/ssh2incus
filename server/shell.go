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

func checkTermMux(tmux *TermMux, c *incus.Client, lu *LoginUser, iu *incus.InstanceUser, env map[string]string) error {
	existsParams := &incus.CommandExistsParams{
		Project:     lu.Project,
		Instance:    lu.Instance,
		Path:        tmux.Name(),
		ShouldCache: true,
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
		io.WriteString(s, fmt.Sprintf("Invalid login for %q (%s)\n", lu.OrigUser, lu))
		s.Exit(ExitCodeInvalidLogin)
		return
	}

	// Only root is allowed to access Incus shell
	if lu.User == "root" && lu.Command == "shell" {
		incusShell(s)
		return
	}

	if lu.IsCommand() {
		log.Warnf("shell: command %q not allowed", lu)
		io.WriteString(s, fmt.Sprintf("%%%s not allowed\n", lu.Command))
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
	if lu.CreateInstance && config.AllowCreate {
		iu, _ = client.GetInstanceUser(lu.Project, lu.Instance, lu.InstanceUser)

		// Only attempt to create an instance if it doesn't exist
		if iu == nil {
			log.Debugf("shell: creating instance %s", lu)
			io.WriteString(s, "creating instance...\n")

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
				io.WriteString(s, fmt.Sprintf("%s\n", err))
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
				io.WriteString(s, "tip: run `sudo poweroff` to destroy ephemeral instance\n")
			}

			log.Debugf("shell: create instance config: %+v", params)

			_, err = client.CreateInstance(params)
			if err != nil {
				log.Warnf("shell: failed to create instance %s: %v", lu, err)
				io.WriteString(s, fmt.Sprintf("cannot create instance:\n%s\n", err))
				s.Exit(ExitCodeInternalError)
				return
			}
			// try this if instance user is not root assuming that it needs to be created by cloud-init
			if lu.InstanceUser != "root" {
				for i := 0; i < 20; i++ {
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

	log.Debugf("shell: connecting %s", lu)

	if iu == nil {
		iu, err = client.GetCachedInstanceUser(lu.Project, lu.Instance, lu.InstanceUser)
		if err != nil {
			log.Errorf("shell: failed to get instance user %q for %s: %s", lu.InstanceUser, lu, err)
			io.WriteString(s, fmt.Sprintf("cannot get instance user %q\n", lu.InstanceUser))
			s.Exit(ExitCodeInvalidLogin)
			return
		}
	}

	if iu == nil {
		log.Errorf("shell: not found instance user for %q", lu)
		io.WriteString(s, fmt.Sprintf("not found user or instance for %q\n", lu))
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
			io.WriteString(s.Stderr(), "failed to setup agent\n")
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
			io.WriteString(s.Stderr(), "failed to setup agent socket\n")
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
			io.WriteString(s.Stderr(), "failed to create persistent session\n")
		}
		err = checkTermMux(tmux, client, lu, iu, env)
		if err != nil {
			log.Errorf("shell: failed to create persistent session: %v", err)
			io.WriteString(s.Stderr(), fmt.Sprintf("failed to create persistent session:\n%s\n", err))
		}

		shouldRunAsUser = true
		cmd = tmux.Attach()
	}

	log.Debugf("shell: CMD %s", oneLine(cmd))
	log.Debugf("shell: PTY %v", isPty)
	log.Debugf("shell: ENV %s", oneLine(util.MapToEnvString(env)))

	if welcome := welcomeHandler(iu); config.Welcome && isPty && !isRaw && welcome != "" {
		s.Write([]byte(fmt.Sprintf("\n%s\n\n", welcome)))
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
		io.WriteString(s, "Internal error: command parsing failed\n")
		s.Exit(ExitCodeConnectionError)
		return
	}

	cmd := exec.Command(args[0], args[1:]...)

	ptyReq, winCh, isPty := s.Pty()
	if !isPty {
		io.WriteString(s, "No PTY requested\n")
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
		io.WriteString(s, "Could not allocate PTY\n")
		s.Exit(-1)
	}
	defer p.Close()

	hostname, _ := os.Hostname()
	io.WriteString(s, fmt.Sprintf(`
incus shell emulator on %s (Ctrl+C to exit)

Hit ENTER or type 'help <command>' for help about any command
`, hostname))
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
			for i := 0; i < n; i++ {
				if buf[i] == 3 {
					log.Debugf("incus shell: received Ctrl+C, exiting")
					io.WriteString(s, "\nExiting incus shell\n")
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
		io.WriteString(s, "Could not allocate PTY\n")
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
