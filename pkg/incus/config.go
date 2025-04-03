package incus

import (
	"fmt"
	"os"
	"strings"

	"github.com/lxc/incus/v6/shared/cliconfig"
)

func RemoteConnectParams(remote string) (*ConnectParams, error) {
	clicfg, err := cliconfig.LoadConfig("")
	if err != nil || clicfg == nil {
		return nil, fmt.Errorf("failed to load Incus CLI config: %w", err)
	}

	if remote == "" {
		remote = clicfg.DefaultRemote
	}

	remoteConfig, ok := clicfg.Remotes[remote]
	if !ok {
		return nil, fmt.Errorf("remote '%s' not found in incus configuration", remote)
	}

	url := remoteConfig.Addr

	var serverCertFile, certFile, keyFile string

	// For HTTPS connections, determine client certificate paths
	if strings.HasPrefix(url, "https://") {
		// Check if custom paths are provided in our config
		serverCertFile = clicfg.ConfigPath("servercerts", remote+".crt")
		// Use default Incus client cert/key which are stored in the same directory as config.yml
		certFile = clicfg.ConfigPath("client.crt")
		keyFile = clicfg.ConfigPath("client.key")

		// Ensure certificate files exist
		if _, err := os.Stat(certFile); err != nil {
			return nil, fmt.Errorf("client certificate not found at %s: %w", certFile, err)
		}
		if _, err := os.Stat(keyFile); err != nil {
			return nil, fmt.Errorf("client key not found at %s: %w", keyFile, err)
		}
	} else if strings.HasPrefix(url, "unix://") {
		url = strings.TrimPrefix(url, "unix://")
	}

	return &ConnectParams{
		Remote:         remote,
		Url:            url,
		CertFile:       certFile,
		KeyFile:        keyFile,
		ServerCertFile: serverCertFile,
	}, nil
}
