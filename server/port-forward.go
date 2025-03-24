package server

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"ssh2incus/pkg/incus"
	"ssh2incus/pkg/util/ssh"

	"github.com/lxc/incus/v6/shared/api"
	log "github.com/sirupsen/logrus"
	gossh "golang.org/x/crypto/ssh"
)

// direct-tcpip data struct as specified in RFC4254, Section 7.2
type localForwardChannelData struct {
	DestAddr string
	DestPort uint32

	OriginAddr string
	OriginPort uint32
}

func directTCPIPHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	lu, ok := ctx.Value("LoginUser").(LoginUser)
	if !ok || !lu.IsValid() {
		log.Errorf("invalid connection data for %#v", lu)
		newChan.Reject(gossh.ConnectionFailed, "invalid connection data")
		conn.Close()
		return
	}

	d := localForwardChannelData{}
	if err := gossh.Unmarshal(newChan.ExtraData(), &d); err != nil {
		log.Errorf("error parsing forward data for %#v %#v: %s", lu, d, err)
		newChan.Reject(gossh.ConnectionFailed, "error parsing forward data: "+err.Error())
		conn.Close()
		return
	}

	if instanceAddr, ok := ctx.Value("ResolvedInstanceAddr").(string); ok {
		log.Debugf("resolved instance address %s for %#v", instanceAddr, lu)
		if d.DestAddr == "" && instanceAddr != "" {
			d.DestAddr = instanceAddr
		}
	} else {
		server, err := NewIncusServer()
		if err != nil {
			log.Errorf("failed to initialize incus client: %w", err)
			newChan.Reject(gossh.ConnectionFailed, "cannot initialize incus client: "+err.Error())
			conn.Close()
			return
		}

		// Connect to Incus
		err = server.Connect(ctx)
		if err != nil {
			log.Errorf("failed to connect to incus: %w", err)
			newChan.Reject(gossh.ConnectionFailed, "cannot connect to incus: "+err.Error())
			conn.Close()
			return
		}
		defer server.Disconnect()

		if !lu.IsDefaultProject() {
			err = server.UseProject(lu.Project)
			if err != nil {
				log.Errorf("using project %s error: %s", lu.Project, err)
				newChan.Reject(gossh.ConnectionFailed, "cannot connect to incus: "+err.Error())
				conn.Close()
				return
			}
		}

		meta, _, err := server.GetInstanceState(lu.Instance)
		if err != nil {
			log.Errorf("failed to get instance state for %#v", lu)
			newChan.Reject(gossh.ConnectionFailed, err.Error())
			conn.Close()
			return
		}

		log.Debugf("Instance State Meta: %#v", meta)

		network := api.InstanceStateNetwork{}
		for d, v := range meta.Network {
			if strings.HasPrefix(d, "e") {
				network = v
				break
			}
		}

		if len(network.Addresses) == 0 {
			log.Errorf("failed to get instance IP address for %#v", lu)
			newChan.Reject(gossh.ConnectionFailed, "")
			conn.Close()
			return
		}

		instanceAddr := network.Addresses[0].Address
		ctx.SetValue("ResolvedInstanceAddr", instanceAddr)

		if d.DestAddr == "" && instanceAddr != "" {
			d.DestAddr = instanceAddr
		}
	}

	if d.DestAddr == "127.0.0.1" {
		server, err := NewIncusServer()
		if err != nil {
			log.Errorf("failed to initialize incus client: %w", err)
			newChan.Reject(gossh.ConnectionFailed, "cannot initialize incus client: "+err.Error())
			conn.Close()
			return
		}

		err = server.Connect(ctx)
		if err != nil {
			log.Errorf("failed to connect to incus: %w", err)
			newChan.Reject(gossh.ConnectionFailed, "cannot connect to incus: "+err.Error())
			conn.Close()
			return
		}
		defer server.Disconnect()

		if !lu.IsDefaultProject() {
			err = server.UseProject(lu.Project)
			if err != nil {
				log.Errorf("using project %s error: %s", lu.Project, err)
				newChan.Reject(gossh.ConnectionFailed, "cannot connect to incus: "+err.Error())
				conn.Close()
				return
			}
		}

		p := server.NewProxyDevice(incus.ProxyDevice{
			Project:  lu.Project,
			Instance: lu.Instance,
			Source:   fmt.Sprintf("%d", d.DestPort),
		})

		if port, err := p.AddPort(); err == nil {
			u64, _ := strconv.ParseUint(port, 10, 32)
			d.DestPort = uint32(u64)
			defer p.RemovePort()
		} else {
			log.Errorf("port forwarding: %w", err)
		}
	}

	dest := net.JoinHostPort(d.DestAddr, strconv.FormatInt(int64(d.DestPort), 10))

	log.Debugf("local port-forwarding on %s for %#v", dest, lu)

	var dialer net.Dialer
	dconn, err := dialer.DialContext(ctx, "tcp", dest)
	if err != nil {
		log.Errorf("failed to connect to tcp://%s: %s", dest, err)
		newChan.Reject(gossh.ConnectionFailed, err.Error())
		return
	}

	ch, reqs, err := newChan.Accept()
	if err != nil {
		log.Errorf("failed to accept new channel request: %s", err)
		dconn.Close()
		return
	}
	go gossh.DiscardRequests(reqs)

	done := make(chan struct{}, 2)
	go func() {
		defer ch.Close()
		defer dconn.Close()
		io.Copy(ch, dconn)
		done <- struct{}{}
	}()
	go func() {
		defer ch.Close()
		defer dconn.Close()
		io.Copy(dconn, ch)
		done <- struct{}{}
	}()

	<-done
	<-done
	log.Debugf("done with local port-forwarding on %s for %#v", dest, lu)
}
