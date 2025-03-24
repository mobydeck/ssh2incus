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
	"github.com/lxc/incus/v6/shared/api"
)

type ConnectParams struct {
	Url            string
	CertFile       string
	KeyFile        string
	ServerCertFile string
	CaCertFile     string
}

type Server struct {
	srv    incus.InstanceServer
	params *ConnectParams
}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) SetConnectParams(p *ConnectParams) {
	s.params = p
}

func (s *Server) Connect(ctx context.Context) error {
	var err error
	params := *s.params
	// Check if the URL is an HTTPS URL
	if strings.HasPrefix(params.Url, "https://") {
		// HTTPS connection requires client certificates
		if params.CertFile == "" || params.KeyFile == "" {
			return fmt.Errorf("client certificate and key files are required for HTTPS connections")
		}

		// Load client certificate and key
		keyPair, err := tls.LoadX509KeyPair(params.CertFile, params.KeyFile)
		if err != nil {
			return fmt.Errorf("failed to load client certificate and key: %w", err)
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
				return fmt.Errorf("failed to marshal EC private key: %w", err)
			}
			keyPEM = pem.EncodeToMemory(&pem.Block{
				Type:  "EC PRIVATE KEY",
				Bytes: keyBytes,
			})
		default:
			// For other types like ed25519, we'd need specific handling
			return fmt.Errorf("unsupported private key type: %T", keyPair.PrivateKey)
		}

		var serverCertPEM []byte
		if params.ServerCertFile != "" {
			serverCertPEM, err = os.ReadFile(params.ServerCertFile)
			if err != nil {
				return fmt.Errorf("failed to read CA cert file: %w", err)
			}
		}

		// Connect using HTTPS
		args := &incus.ConnectionArgs{
			TLSClientCert: string(certPEM),
			TLSClientKey:  string(keyPEM),
			TLSServerCert: string(serverCertPEM),
		}
		s.srv, err = incus.ConnectIncusWithContext(ctx, params.Url, args)
		return err
	} else {
		// If not HTTPS, treat as Unix socket path
		s.srv, err = incus.ConnectIncusUnixWithContext(ctx, params.Url, nil)
		return err
	}
}

func (s *Server) UseProject(project string) error {
	_, _, err := s.srv.GetProject(project)
	if err != nil {
		return err
	}
	s.srv = s.srv.UseProject(project)
	return nil
}

func (s *Server) GetInstance(name string) (*api.Instance, string, error) {
	return s.srv.GetInstance(name)
}

func (s *Server) GetInstanceState(name string) (*api.InstanceState, string, error) {
	return s.srv.GetInstanceState(name)
}

func (s *Server) UpdateInstance(name string, instance api.InstancePut, ETag string) (incus.Operation, error) {
	return s.srv.UpdateInstance(name, instance, ETag)
}

func IsDefaultProject(project string) bool {
	if project == "" || project == "default" {
		return true
	}
	return false
}

func (s *Server) GetConnectionInfo() map[string]interface{} {
	info, _ := s.srv.GetConnectionInfo()
	return structs.Map(info)
}

func (s *Server) Disconnect() {
	s.srv.Disconnect()
}
