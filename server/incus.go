package server

import (
	"context"
	"fmt"
	"os"
	"strings"

	"ssh2incus/pkg/incus"

	"github.com/lxc/incus/v6/shared/cliconfig"
	log "github.com/sirupsen/logrus"
)

var DefaultParams *incus.ConnectParams = nil
var incusConnectParams *incus.ConnectParams

func NewIncusClient(params *incus.ConnectParams) (*incus.Client, error) {
	var err error
	if params == nil {
		params, err = getIncusConnectParams()
		if err != nil {
			return nil, err
		}
	}
	s := incus.NewClientWithParams(params)
	return s, nil
}

func NewIncusClientWithContext(ctx context.Context, params *incus.ConnectParams) (*incus.Client, error) {
	server, err := NewIncusClient(DefaultParams)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize incus client: %v", err)
	}

	err = server.Connect(ctx)
	if err != nil {
		server.Disconnect()
		return nil, fmt.Errorf("failed to connect to incus: %v", err)
	}
	return server, nil
}

func (s *Server) checkIncus() error {
	client, err := NewIncusClientWithContext(context.Background(), DefaultParams)
	if err != nil {
		return fmt.Errorf("failed to connect to incus: %w", err)
	}
	defer client.Disconnect()

	info := client.GetConnectionInfo()
	config.IncusInfo = info
	log.Debugln(info)

	return nil
}

func getIncusConnectParams() (*incus.ConnectParams, error) {
	if incusConnectParams != nil {
		return incusConnectParams, nil
	}

	clicfg, err := cliconfig.LoadConfig("")
	if err != nil {
		log.Debugf("Failed to load incus CLI config: %v", err)
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

	incusConnectParams = &incus.ConnectParams{
		Url:            url,
		CertFile:       certFile,
		KeyFile:        keyFile,
		ServerCertFile: serverCertFile,
	}
	return incusConnectParams, nil
}
