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
	"sync"
	"time"

	"ssh2incus/pkg/cache"
	"ssh2incus/pkg/queue"
	"ssh2incus/pkg/util/structs"

	incus "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/lxc/incus/v6/shared/cliconfig"
)

var (
	instanceStateCache *cache.Cache
	instanceStateQueue *queue.Queueable[*api.InstanceState]
	instanceStateOnce  sync.Once
)

func init() {
	instanceStateOnce.Do(func() {
		instanceStateCache = cache.New(1*time.Minute, 2*time.Minute)
		instanceStateQueue = queue.New[*api.InstanceState](100)
	})
}

type ConnectParams struct {
	Remote         string
	Url            string
	CertFile       string
	KeyFile        string
	ServerCertFile string
	CaCertFile     string
}

type Client struct {
	srv     incus.InstanceServer
	params  *ConnectParams
	project string
}

func NewClientWithParams(p *ConnectParams) *Client {
	c := new(Client)
	c.params = p
	return c
}

func (c *Client) Connect(ctx context.Context) error {
	var err error
	params := *c.params
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
			SkipGetServer: true,
		}
		c.srv, err = incus.ConnectIncusWithContext(ctx, params.Url, args)
		return err
	} else {
		// If not HTTPS, treat as Unix socket path
		c.srv, err = incus.ConnectIncusUnixWithContext(ctx, params.Url, nil)
		return err
	}
}

func (c *Client) UseProject(project string) error {
	if project == "" {
		project = "default"
	}
	if project == c.project {
		return nil
	}
	p, _, err := c.srv.GetProject(project)
	if err != nil {
		return err
	}
	project = p.Name
	c.srv = c.srv.UseProject(project)
	c.project = project
	return nil
}

func (c *Client) GetConnectionInfo() map[string]interface{} {
	info, _ := c.srv.GetConnectionInfo()
	return structs.Map(info)
}

func (c *Client) Disconnect() {
	c.srv.Disconnect()
}

// GetProjects returns all projects
func (c *Client) GetProjects() ([]api.Project, error) {
	return c.srv.GetProjects()
}

// ListProjects returns a list of available projects
func (c *Client) ListProjects() ([]api.Project, error) {
	return c.srv.GetProjects()
}

// ListImages returns a list of images from a remote
func (c *Client) ListImages(remote string) ([]api.Image, error) {
	if remote == "" {
		remote = "images"
	}

	cc := cliconfig.Config{
		Remotes: map[string]cliconfig.Remote{
			"images": cliconfig.ImagesRemote,
		},
	}

	imgServer, err := cc.GetImageServer(remote)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to images remote: %v", err)
	}

	// Get image aliases (which represent available images)
	aliases, err := imgServer.GetImageAliases()
	if err != nil {
		return nil, fmt.Errorf("failed to get image aliases: %v", err)
	}

	// Convert aliases to image info
	var images []api.Image
	seen := make(map[string]bool)

	for _, alias := range aliases {
		// Skip duplicates (same fingerprint)
		if seen[alias.Target] {
			continue
		}
		seen[alias.Target] = true

		// Get full image info
		img, _, err := imgServer.GetImage(alias.Target)
		if err != nil {
			continue // Skip images we can't fetch
		}
		images = append(images, *img)
	}

	return images, nil
}

// InstanceExists checks if an instance exists in the specified project
func (c *Client) InstanceExists(name, project string) (bool, error) {
	err := c.UseProject(project)
	if err != nil {
		return false, err
	}

	_, _, err = c.srv.GetInstance(name)
	if err == nil {
		return true, nil
	}

	// Check if it's a "not found" error
	if strings.Contains(err.Error(), "not found") {
		return false, nil
	}

	return false, err
}

// GetInstanceServer returns the underlying InstanceServer for direct access
func (c *Client) GetInstanceServer() incus.InstanceServer {
	return c.srv
}

func IsDefaultProject(project string) bool {
	if project == "" || project == "default" {
		return true
	}
	return false
}
