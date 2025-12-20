package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"ssh2incus/pkg/cron"
	"ssh2incus/pkg/ssh"

	log "github.com/sirupsen/logrus"
	gossh "golang.org/x/crypto/ssh"
)

const sessionChannel = "session"
const defaultSubsystem = "default"

type contextKey struct {
	name string
}

type Server struct{}

func WithConfig(c *Config) *Server {
	config = c
	return new(Server)
}

func Run() {
	new(Server).Run()
}

// Run initializes and starts the server based on the configuration.
// It determines whether to serve as a child process, master, or daemon.
func (s *Server) Run() {
	if os.Getenv(config.SocketFdEnvName()) != "" {
		log.Infof("%s child process, pid %d", config.App.Name(), os.Getpid())
		s.Serve()
		os.Exit(0)
	}

	starting := fmt.Sprintf("starting %s on %s as %%s, pid %d", config.App.Name(), config.Listen, os.Getpid())
	if config.Master {
		log.Infof(starting, "master process")
		s.Listen()
	} else {
		log.Infof(starting, "daemon")
		s.ListenAndServe()
	}

}

// ListenAndServe initializes and starts the server, manages health checks,
// and handles graceful shutdown upon receiving termination signals.
func (s *Server) ListenAndServe() {
	if err := s.checkIncus(); err != nil {
		log.Fatal(err)
	}

	if len(config.HealthCheck) > 0 {
		s.enableHealthCheck()
	}

	server := setupServer()

	// Set up a channel to listen for signals
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	err := cleanLeftoverProxyDevices()
	if err != nil {
		log.Errorf("clean leftover devices: %v", err)
	}

	// Start the server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Wait for a signal to gracefully shutdown
	<-stop
	log.Info("shutting down server...")

	// Create a context with a 5 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Debugf("shutting down devices...")
	err = deviceRegistry.ShutdownAllDevices(ctx)
	if err != nil {
		log.Errorf("failed to shutdown devices: %v", err)
	}

	// Perform graceful shutdown
	if err = server.Shutdown(ctx); err != nil {
		log.Fatalf("server shutdown failed: %v", err)
	}

	log.Info("server gracefully stopped")
}

// Listen starts a TCP server listening on the configured address.
// It accepts incoming connections and hands them off to child processes.
func (s *Server) Listen() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	listener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		log.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				} else {
					log.Errorf("failed to accept connection: %v", err)
					continue
				}
			}

			log.Info("connection accepted, handing off to child process")
			go handoffToChild(conn)
		}
	}()

	<-stop
	log.Info("master server stopped")
}

// Serve sets up and runs the SSH server, handling a connection passed via a file descriptor for a child process.
func (s *Server) Serve() {
	signal.Ignore(syscall.SIGINT, syscall.SIGTERM)

	server := setupServer()

	// The file descriptor is 3 in the child process (0=stdin, 1=stdout, 2=stderr)
	// Since we passed it via ExtraFiles in the parent
	actualFd := 3

	// Create a new file from the file descriptor
	file := os.NewFile(uintptr(actualFd), config.App.Name()+"-connection")
	if file == nil {
		log.Fatal("failed to create file from descriptor")
	}
	defer file.Close()

	// Convert the file back to a TCP connection
	conn, err := net.FileConn(file)
	if err != nil {
		log.Fatalf("failed to create connection from file: %v", err)
	}
	defer conn.Close()
	log.Infof("child process serving connection from %s", conn.RemoteAddr())

	ctx, cancel := ssh.NewContext(server)
	ctx.SetValue(ssh.ContextKeyCancelFunc, cancel)

	go server.HandleConnWithContext(conn, ctx)

	<-ctx.Done()

	// Create a context with a 5 second timeout
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	log.Debugf("shutting down devices...")
	err = deviceRegistry.ShutdownAllDevices(cleanupCtx)
	if err != nil {
		log.Errorf("failed to shutdown devices: %v", err)
	}

	log.Infof("%s child process ended, pid %d", config.App.Name(), os.Getpid())
}

func handoffToChild(conn net.Conn) {
	// Get the TCP connection's file descriptor
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		log.Errorf("failed to convert to TCPConn")
		conn.Close()
		return
	}

	// Get the underlying file
	file, err := tcpConn.File()
	if err != nil {
		log.Errorf("failed to get file from connection: %v", err)
		conn.Close()
		return
	}
	defer file.Close()

	// The parent doesn't need the connection anymore
	conn.Close()

	//execPath, err := os.Executable()
	//if err != nil {
	//	log.Println("Failed to get executable path:", err)
	//	return
	//}

	//Start the child process, passing the file descriptor as an environment variable
	//cmd := exec.Command(execPath, os.Args[1:]...)

	cmd := exec.Command(os.Args[0], "serve", conn.RemoteAddr().String())
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, config.SocketFdEnvName()+"=") {
			continue
		}
		env = append(env, e)
	}
	cmd.Env = append(env,
		config.SocketFdEnvString(file),
		config.ArgsEnvString(),
	)

	// Set the file descriptor to be inherited by the child
	cmd.ExtraFiles = []*os.File{file}

	// Connect standard output and error
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Set process attributes to detach the child process
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// Set detach flag (different per OS)
		Setpgid: true, // On Unix/Linux
		// For Windows, you would use:
		// CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		//Noctty: true,
	}

	//Start the process
	if err := cmd.Start(); err != nil {
		log.Errorf("failed to start child process: %v", err)
		return
	}

	//We don't need to wait for the child at all, since it's detached
	//Just let the process ID be collected by the OS
	log.Infof("started detached child process, pid %d", cmd.Process.Pid)
	//cmd.Process.Release()
	// Wait allows avoid <defunct> processes
	cmd.Process.Wait()

	// Another approach

	//cwd, _ := os.Getwd()
	//var attr = os.ProcAttr{
	//	Dir: cwd,
	//	Env: append(os.Environ(), "SOCKET_FD="+strconv.Itoa(int(file.Fd()))),
	//	Files: []*os.File{
	//		os.Stdin,
	//		os.Stdout,
	//		os.Stderr,
	//		file,
	//	},
	//	Sys: &syscall.SysProcAttr{
	//		// Set detach flag (different per OS)
	//		Setpgid: true, // On Unix/Linux
	//		// For Windows, you would use:
	//		// CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	//		//Noctty: true,
	//	},
	//}
	//process, err := os.StartProcess(execPath, os.Args[1:], &attr)
	//if err == nil {
	//	// It is not clear from docs, but Realease actually detaches the process
	//	err = process.Release()
	//	if err != nil {
	//		log.Error(err.Error())
	//		return
	//	}
	//} else {
	//	log.Error(err.Error())
	//	return
	//}
	//
	//log.Printf("Started detached child process with PID: %d", process.Pid)
}

func setupServer() *ssh.Server {
	var publickeyHandler ssh.PublicKeyHandler

	switch {
	case config.InstanceAuth:
		publickeyHandler = instanceAuthHandler
	case config.NoAuth:
		publickeyHandler = noAuthHandler
	default:
		publickeyHandler = hostAuthHandler
	}

	if !config.NoAuth && len(config.AllowedGroups) > 0 {
		config.AllowedGroups = append([]string{"0"}, getGroupIds(config.AllowedGroups)...)
	}

	var hostSigners []ssh.Signer
	if keyFiles, err := filepath.Glob("/etc/ssh/ssh_host_*_key"); err == nil {
		for _, file := range keyFiles {
			privateBytes, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			private, err := gossh.ParsePrivateKey(privateBytes)
			if err != nil {
				continue
			}
			hostSigners = append(hostSigners, private)
		}
	}

	forwardHandler := new(ForwardTCPHandler)

	server := &ssh.Server{
		Addr:        config.Listen,
		IdleTimeout: config.IdleTimeout,
		Version:     "Incus",
		Handler:     shellHandler,
		ChannelHandlers: map[string]ssh.ChannelHandler{
			sessionChannel:     ssh.DefaultSessionHandler,
			directTCPIPChannel: directTCPIPStdioHandler,
		},
		SubsystemHandlers: map[string]ssh.SubsystemHandler{
			defaultSubsystem: defaultSubsystemHandler,
			sftpSubsystem:    sftpSubsystemHandler,
		},
		RequestHandlers: map[string]ssh.RequestHandler{
			tcpipForwardRequest:       forwardHandler.HandleSSHRequest,
			tcpipForwardCancelRequest: forwardHandler.HandleSSHRequest,
		},
		ReversePortForwardingCallback: reversePortForwardingCallback,
		HostSigners:                   hostSigners,
	}

	if len(config.AuthMethods) > 0 {
		server.AuthMethods = config.AuthMethods
		server.PasswordHandler = passwordHandler
	}

	server.PublicKeyHandler = publickeyHandler

	if config.PassAuth && !config.NoAuth {
		server.PasswordHandler = passwordHandler
	}

	if config.Banner {
		server.BannerHandler = bannerHandler
	}
	return server
}

func (s *Server) enableHealthCheck() {
	c := cron.New()
	_, err := c.AddFunc(
		fmt.Sprintf("@every %s", config.HealthCheck),
		func() {
			s.checkHealth()
		})
	if err != nil {
		log.Errorf("failed to add health check: %v", err)
		return
	}
	c.Start()
}

func (s *Server) checkHealth() bool {
	err := s.checkIncus()
	if err != nil {
		log.Errorf("health check failed: %v", err)
	}
	return err == nil
}
