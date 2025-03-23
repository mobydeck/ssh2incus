package incus

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"ssh2incus/pkg/util/structs"

	"github.com/lxc/incus/v6/client"
)

type ConnectParams struct {
	Url            string
	CertFile       string
	KeyFile        string
	ServerCertFile string
	CaCertFile     string
}

func Connect(ctx context.Context, params *ConnectParams) (incus.InstanceServer, error) {
	// Check if the URL is an HTTPS URL
	if strings.HasPrefix(params.Url, "https://") {
		// HTTPS connection requires client certificates
		if params.CertFile == "" || params.KeyFile == "" {
			return nil, fmt.Errorf("client certificate and key files are required for HTTPS connections")
		}

		// Load client certificate and key
		keyPair, err := tls.LoadX509KeyPair(params.CertFile, params.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate and key: %w", err)
		}

		certPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: keyPair.Certificate[0],
		})

		// Convert the private key to PEM format
		// We need to determine the type of private key and encode accordingly
		var keyPEM []byte
		switch key := keyPair.PrivateKey.(type) {
		case *rsa.PrivateKey:
			keyPEM = pem.EncodeToMemory(&pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: x509.MarshalPKCS1PrivateKey(key),
			})
		case *ecdsa.PrivateKey:
			keyBytes, err := x509.MarshalECPrivateKey(key)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal EC private key: %w", err)
			}
			keyPEM = pem.EncodeToMemory(&pem.Block{
				Type:  "EC PRIVATE KEY",
				Bytes: keyBytes,
			})
		default:
			// For other types like ed25519, we'd need specific handling
			return nil, fmt.Errorf("unsupported private key type: %T", keyPair.PrivateKey)
		}

		var serverCertPEM []byte
		if params.ServerCertFile != "" {
			serverCertPEM, err = os.ReadFile(params.ServerCertFile)
			if err != nil {
				return nil, fmt.Errorf("failed to read CA cert file: %w", err)
			}
		}

		// Connect using HTTPS
		args := &incus.ConnectionArgs{
			TLSClientCert: string(certPEM),
			TLSClientKey:  string(keyPEM),
			TLSServerCert: string(serverCertPEM),
		}

		return incus.ConnectIncusWithContext(ctx, params.Url, args)
	} else {
		// If not HTTPS, treat as Unix socket path
		return incus.ConnectIncusUnixWithContext(ctx, params.Url, nil)
	}
}

func UseProject(server incus.InstanceServer, project string) (incus.InstanceServer, error) {
	_, _, err := server.GetProject(project)
	if err != nil {
		return nil, err
	}
	p := server.UseProject(project)
	return p, nil
}

func IsDefaultProject(project string) bool {
	if project == "" || project == "default" {
		return true
	}
	return false
}

func GetConnectionInfo(c incus.InstanceServer) map[string]interface{} {
	info, _ := c.GetConnectionInfo()
	return structs.Map(info)
}
