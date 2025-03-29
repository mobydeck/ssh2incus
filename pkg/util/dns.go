package util

import (
	"context"
	"fmt"
	"net"
	"time"
)

type DNSResolver struct {
	Timeout time.Duration
	Server  string
}

func NewDNSResolver() *DNSResolver {
	return &DNSResolver{}
}

func (r *DNSResolver) LookupHost(hostname string) ([]net.IP, error) {
	if r.Timeout == 0 {
		r.Timeout = 2 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), r.Timeout)
	defer cancel()

	dialFn := func(ctx context.Context, network, address string) (net.Conn, error) {
		d := net.Dialer{
			Timeout: r.Timeout,
		}
		return d.DialContext(ctx, network, address)
	}

	if r.Server != "" {
		dialFn = func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: r.Timeout,
			}
			return d.DialContext(ctx, network, r.Server)
		}
	}

	// Create a custom resolver
	resolver := &net.Resolver{
		PreferGo: true,
		Dial:     dialFn,
	}

	addrs, err := resolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", hostname, err)
	}

	ips := make([]net.IP, len(addrs))
	for i, addr := range addrs {
		ips[i] = addr.IP
	}

	return ips, nil
}
