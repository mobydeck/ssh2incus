package server

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	"ssh2incus/pkg/incus"
	"ssh2incus/pkg/ssh"

	log "github.com/sirupsen/logrus"
	gossh "golang.org/x/crypto/ssh"
)

const (
	forwardedTCPChannelType   = "forwarded-tcpip"
	tcpipForwardRequest       = "tcpip-forward"
	tcpipForwardCancelRequest = "cancel-tcpip-forward"
)

type remoteForwardRequest struct {
	BindAddr string
	BindPort uint32
}

type remoteForwardSuccess struct {
	BindPort uint32
}

type remoteForwardCancelRequest struct {
	BindAddr string
	BindPort uint32
}

type remoteForwardChannelData struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

func (r *remoteForwardRequest) String() string {
	return fmt.Sprintf("(bindAddr=%s, bindPort=%d)",
		r.BindAddr, r.BindPort)
}

func (d *remoteForwardChannelData) String() string {
	return fmt.Sprintf("(destAddr=%s, destPort=%d, originAddr=%s, originPort=%d)",
		d.DestAddr, d.DestPort, d.OriginAddr, d.OriginPort)
}

func reversePortForwardingCallback(ctx ssh.Context, host string, port uint32) bool {
	lu := LoginUserFromContext(ctx)
	if lu.IsValid() {
		log.Infof("requested reverse port forwarding on %s:%d for %s", host, port, lu)
	}
	return true
}

// ForwardTCPHandler can be enabled by creating a ForwardTCPHandler and
// adding the HandleSSHRequest callback to the server's RequestHandlers under
// tcpip-forward and cancel-tcpip-forward.
type ForwardTCPHandler struct {
	forwards map[string]net.Listener
	sync.Mutex
}

func (h *ForwardTCPHandler) HandleSSHRequest(ctx ssh.Context, srv *ssh.Server, req *gossh.Request) (bool, []byte) {
	log := log.WithField("session", ctx.ShortSessionID())

	lu := LoginUserFromContext(ctx)
	log.Debugf("handle ssh request %s for %s", req.Type, lu)
	if !lu.IsValid() {
		log.Errorf("invalid login for %s", lu)
		return false, []byte(fmt.Sprintf("Invalid login for %q (%s)", lu.OrigUser, lu))
	}

	h.Lock()
	if h.forwards == nil {
		h.forwards = make(map[string]net.Listener)
	}
	h.Unlock()
	conn := ctx.Value(ssh.ContextKeyConn).(*gossh.ServerConn)
	switch req.Type {
	case tcpipForwardRequest:
		reqPayload := new(remoteForwardRequest)
		if err := gossh.Unmarshal(req.Payload, reqPayload); err != nil {
			log.Errorf("parse %s payload failed: %v", tcpipForwardRequest, err)
			return false, []byte("error parsing payload\n")
		}
		if srv.ReversePortForwardingCallback == nil || !srv.ReversePortForwardingCallback(ctx, reqPayload.BindAddr, reqPayload.BindPort) {
			return false, []byte("port forwarding is disabled\n")
		}

		client, err := NewDefaultIncusClientWithContext(ctx)
		if err != nil {
			log.Error(err)
			return false, []byte(fmt.Sprintf("cannot connect to incus: %v\n", err))
		}
		defer client.Disconnect()

		err = client.UseProject(lu.Project)
		if err != nil {
			log.Errorf("using project %s error: %s", lu.Project, err)
			return false, []byte(fmt.Sprintf("cannot connect to incus: %v\n", err))
		}

		src := net.JoinHostPort(reqPayload.BindAddr, strconv.Itoa(int(reqPayload.BindPort)))
		pd := client.NewProxyDevice(incus.ProxyDevice{
			Project:  lu.Project,
			Instance: lu.Instance,
			Source:   src,
		})

		_, err = pd.AddReversePort()
		if err != nil {
			log.Errorf("failed to add reverse port for %s: %v", lu, err)
			return false, []byte(fmt.Sprintf("error listening on %s\n", reqPayload.BindAddr))
		}
		deviceRegistry.AddDevice(pd)
		ln := pd.Listener()

		if ln == nil {
			log.Errorf("reverse listener failed for %s %s", lu, reqPayload)
			return false, []byte(fmt.Sprintf("error listening on %s\n", src))
		}
		destAddr, destPortStr, _ := net.SplitHostPort(ln.Addr().String())
		destPort, _ := strconv.Atoi(destPortStr)

		log.Debugf("reverse port forwarding on %s for %s (host %s:%d)", src, lu,
			destAddr, destPort)

		h.Lock()
		h.forwards[src] = ln
		h.Unlock()
		go func() {
			<-ctx.Done()
			log.Debugf("closing listener for reverse port forwarding on %s for %s", src, lu)
			h.Lock()
			pd.Shutdown()
			ln, ok := h.forwards[src]
			h.Unlock()
			if ok {
				ln.Close()
			}
		}()
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					if errors.Is(err, net.ErrClosed) {
						log.Debugf("listener closed for reverse port forwarding on %s for %s", src, lu)
						break
					}
					log.Errorf("accept failed: %v", err)
					break
				}
				originAddr, originPortStr, _ := net.SplitHostPort(c.RemoteAddr().String())
				originPort, _ := strconv.Atoi(originPortStr)
				forwardData := &remoteForwardChannelData{
					DestAddr:   reqPayload.BindAddr,
					DestPort:   reqPayload.BindPort,
					OriginAddr: originAddr,
					OriginPort: uint32(originPort),
				}
				payload := gossh.Marshal(forwardData)
				go func() {
					ch, reqs, err := conn.OpenChannel(forwardedTCPChannelType, payload)
					if err != nil {
						log.Errorf("open %s channel failed on %s: %v", forwardedTCPChannelType, forwardData, err)
						c.Close()
						return
					}
					go gossh.DiscardRequests(reqs)
					go func() {
						defer ch.Close()
						defer c.Close()
						io.Copy(ch, c)
					}()
					go func() {
						defer ch.Close()
						defer c.Close()
						io.Copy(c, ch)
					}()
				}()
			}
			h.Lock()
			pd.Shutdown()
			delete(h.forwards, src)
			h.Unlock()
		}()
		return true, gossh.Marshal(&remoteForwardSuccess{uint32(destPort)})

	case tcpipForwardCancelRequest:
		var reqPayload remoteForwardCancelRequest
		if err := gossh.Unmarshal(req.Payload, &reqPayload); err != nil {
			log.Errorf("parse %s payload failed: %v", tcpipForwardCancelRequest, err)
			return false, []byte("error parsing payload\n")
		}
		addr := net.JoinHostPort(reqPayload.BindAddr, strconv.Itoa(int(reqPayload.BindPort)))
		log.Debugf("cancel reverse port forwarding on %s for %s", addr, lu)
		h.Lock()
		ln, ok := h.forwards[addr]
		h.Unlock()
		if ok {
			ln.Close()
		}
		return true, nil
	default:
		log.Errorf("unknown ssh request %#v", req)
		return false, nil
	}
}
