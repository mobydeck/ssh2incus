package server

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"ssh2incus/pkg/incus"
	"ssh2incus/pkg/util/ssh"

	"github.com/lxc/incus/v6/shared/cliconfig"
	log "github.com/sirupsen/logrus"
	gossh "golang.org/x/crypto/ssh"
	"gopkg.in/robfig/cron.v2"
)

const (
	ShellSu    = "su"
	ShellLogin = "login"
)

type Config struct {
	IdleTimeout   time.Duration
	Debug         bool
	Banner        bool
	Listen        string
	Socket        string
	Noauth        bool
	Shell         string
	Groups        string
	HealthCheck   string
	AllowedGroups []string
	IncusSocket   string
	Remote        string
	URL           string
	ClientCert    string
	ClientKey     string
	ServerCert    string

	IncusInfo map[string]interface{}
}

var config *Config

var connectParams *incus.ConnectParams

func Run(c *Config) {
	config = c

	if err := checkIncus(); err != nil {
		log.Fatal(err.Error())
	}

	if len(config.HealthCheck) > 0 {
		enableHealthCheck()
	}

	var authHandler ssh.PublicKeyHandler
	if config.Noauth {
		authHandler = noAuthHandler
	} else {
		authHandler = keyAuthHandler
	}

	if !config.Noauth && len(config.AllowedGroups) > 0 {
		config.AllowedGroups = append([]string{"0"}, getGroupIds(c.AllowedGroups)...)
	}

	var defaultSubsystemHandler ssh.SubsystemHandler = defaultSubsystemHandler
	var sftpSubsystemHandler ssh.SubsystemHandler = sftpSubsystemHandler

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

	server := &ssh.Server{
		Addr:             config.Listen,
		IdleTimeout:      config.IdleTimeout,
		Version:          "Incus",
		PublicKeyHandler: authHandler,
		Handler:          shellHandler,
		SubsystemHandlers: map[string]ssh.SubsystemHandler{
			"default": defaultSubsystemHandler,
			"sftp":    sftpSubsystemHandler,
		},
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"session":      ssh.DefaultSessionHandler,
			"direct-tcpip": directTCPIPHandler,
		},
		HostSigners: hostSigners,
	}

	if config.Banner {
		server.BannerHandler = bannerHandler
	}

	// Set up a channel to listen for signals
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Start the server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != ssh.ErrServerClosed {
			log.Fatalf("Server error: %w", err)
		}
	}()

	// Wait for a signal to gracefully shutdown
	<-stop
	log.Info("Shutting down server...")

	// Create a context with a 5 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := deviceRegistry.ShutdownAllDevices(ctx)
	if err != nil {
		log.Errorf("Failed to shutdown devices: %w", err)
	}

	// Perform graceful shutdown
	if err = server.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %w", err)
	}

	log.Info("Server gracefully stopped")

}

func enableHealthCheck() {
	c := cron.New()
	c.AddFunc(fmt.Sprintf("@every %s", config.HealthCheck), checkHealth)
	c.Start()
}

func checkIncus() error {
	server, err := NewIncusServer()
	if err != nil {
		return fmt.Errorf("failed to initialize incus client: %w", err)
	}

	// Connect to Incus
	err = server.Connect(context.Background())
	if err != nil {
		return fmt.Errorf("failed to connect to incus: %w", err)
	}
	defer server.Disconnect()

	info := server.GetConnectionInfo()
	config.IncusInfo = info
	log.Debugln(info)

	return nil
}

func NewIncusServer() (*incus.Server, error) {
	params, err := getIncusServerParams()
	if err != nil {
		return nil, err
	}
	s := incus.NewServer()
	s.SetConnectParams(params)
	return s, nil
}

func getIncusServerParams() (*incus.ConnectParams, error) {
	if connectParams != nil {
		return connectParams, nil
	}

	clicfg, err := cliconfig.LoadConfig("")
	if err != nil {
		log.Debugf("Failed to load incus CLI config: %w", err)
	}

	var url string
	var certFile, keyFile, serverCertFile string

	// First priority: Check if Remote is set
	if config.Remote != "" && clicfg != nil {
		remote, ok := clicfg.Remotes[config.Remote]
		if !ok {
			return nil, fmt.Errorf("remote '%s' not found in incus configuration", config.Remote)
		}
		url = remote.Addr

		// For HTTPS connections, determine client certificate paths
		if strings.HasPrefix(url, "https://") {
			// Check if custom paths are provided in our config
			if config.ServerCert != "" {
				serverCertFile = config.ServerCert
			} else {
				serverCertFile = clicfg.ConfigPath("servercerts", config.Remote+".crt")
			}
			if config.ClientCert != "" && config.ClientKey != "" {
				certFile = config.ClientCert
				keyFile = config.ClientKey
			} else {
				// Use default Incus client cert/key which are stored in the same directory as config.yml
				certFile = clicfg.ConfigPath("client.crt")
				keyFile = clicfg.ConfigPath("client.key")
			}

			// Ensure certificate files exist
			if _, err := os.Stat(certFile); err != nil {
				return nil, fmt.Errorf("client certificate not found at %s: %w", certFile, err)
			}
			if _, err := os.Stat(keyFile); err != nil {
				return nil, fmt.Errorf("client key not found at %s: %w", keyFile, err)
			}
		}
	} else if config.URL != "" {
		// Second priority: Use URL if set
		url = config.URL

		// For HTTPS connections, we need to get cert/key from config or environment
		if strings.HasPrefix(url, "https://") {
			// First try config fields
			if config.ServerCert != "" {
				certFile = config.ServerCert
			} else {
				certFile = os.Getenv("INCUS_SERVER_CERT")
			}
			if config.ClientCert != "" && config.ClientKey != "" {
				certFile = config.ClientCert
				keyFile = config.ClientKey
			} else {
				// Otherwise try environment variables
				certFile = os.Getenv("INCUS_CLIENT_CERT")
				keyFile = os.Getenv("INCUS_CLIENT_KEY")
			}

			if certFile == "" || keyFile == "" {
				return nil, fmt.Errorf("HTTPS connection requires client certificate and key")
			}
		}
	} else if config.Socket != "" {
		// Third priority: Use Socket if set
		url = config.Socket
	} else {
		// Default: Let Incus client use default socket path
		url = ""
	}

	connectParams = &incus.ConnectParams{
		Url:            url,
		CertFile:       certFile,
		KeyFile:        keyFile,
		ServerCertFile: serverCertFile,
	}
	return connectParams, nil
}

func checkHealth() {
	err := checkIncus()
	if err != nil {
		log.Errorf("Health check failed: %w", err)
	}
}

func defaultSubsystemHandler(s ssh.Session) {
	s.Write([]byte(fmt.Sprintf("%s subsytem not implemented\n", s.Subsystem())))
	s.Exit(ExitCodeNotImplemented)
}
