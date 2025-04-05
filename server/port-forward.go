package server

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"ssh2incus/pkg/incus"
	"ssh2incus/pkg/ssh"
	"ssh2incus/pkg/util"
	"ssh2incus/server/stdio-proxy-binary"

	"github.com/lxc/incus/v6/shared/api"
	log "github.com/sirupsen/logrus"
	gossh "golang.org/x/crypto/ssh"
)

const directTCPIPChannel = "direct-tcpip"

var (
	ContextKeyResolvedInstanceAddr = &contextKey{"resolvedInstanceAddress"}
)

// direct-tcpip data struct as specified in RFC4254, Section 7.2
type localForwardChannelData struct {
	DestAddr string
	DestPort uint32

	OriginAddr string
	OriginPort uint32
}

func (d *localForwardChannelData) String() string {
	return fmt.Sprintf("(destAddr=%s, destPort=%d, originAddr=%s, originPort=%d)",
		d.DestAddr, d.DestPort, d.OriginAddr, d.OriginPort)
}

func directTCPIPHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	log := log.WithField("session", ctx.ShortSessionID())

	lu := LoginUserFromContext(ctx)
	log.Debugf("direct-tcpip channel for %s", lu)
	if !lu.IsValid() {
		log.Errorf("invalid login for %s", lu)
		newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("Invalid login for %q (%s)", lu.OrigUser, lu))
		return
	}

	forwardData := &localForwardChannelData{}
	if err := gossh.Unmarshal(newChan.ExtraData(), forwardData); err != nil {
		log.Errorf("error parsing forward data for %s %s: %s", lu, newChan.ExtraData(), err)
		newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("error parsing forward data: %v", err))
		return
	}

	log.Debugf("direct-tcpip channel data for %s: %s", lu, forwardData)

	destAddr := forwardData.DestAddr
	destPort := forwardData.DestPort
	instanceAddr, ok := ctx.Value(ContextKeyResolvedInstanceAddr).(string)
	if !ok {
		client, err := NewDefaultIncusClientWithContext(ctx)
		if err != nil {
			log.Error(err)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("cannot connect to incus: %v", err))
			return
		}
		defer client.Disconnect()

		err = client.UseProject(lu.Project)
		if err != nil {
			log.Errorf("using project %s error: %s", lu.Project, err)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("cannot use project %s: %v", lu.Project, err))
			return
		}

		networks, err := client.GetInstanceNetworks(lu.Project, lu.Instance)
		if err != nil {
			log.Errorf("failed to get instance state for %s", lu)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("cannot get instance state: %v", err))
			return
		}

		network := api.InstanceStateNetwork{}
		for d, v := range networks {
			if strings.HasPrefix(d, "e") {
				network = v
				break
			}
		}

		if len(network.Addresses) == 0 {
			log.Errorf("failed to get instance IP address for %s", lu)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("cannot get instance IP address: %v", err))
			return
		}

		instanceAddr = network.Addresses[0].Address
		ctx.SetValue(ContextKeyResolvedInstanceAddr, instanceAddr)
	}
	log.Debugf("resolved instance address %s for %s", instanceAddr, lu)
	if destAddr == "" && instanceAddr != "" {
		destAddr = instanceAddr
	}

	dest := net.JoinHostPort(destAddr, strconv.Itoa(int(destPort)))

	// if connection is not to instance ip we need to create proxy device
	if destAddr != instanceAddr {
		client, err := NewDefaultIncusClientWithContext(ctx)
		if err != nil {
			log.Error(err)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("cannot connect to incus: %v", err))
			return
		}
		defer client.Disconnect()

		if !lu.IsDefaultProject() {
			err = client.UseProject(lu.Project)
			if err != nil {
				log.Errorf("using project %s error: %s", lu.Project, err)
				newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("cannot connect to incus: %v", err))
				return
			}
		}

		src := net.JoinHostPort(forwardData.DestAddr, strconv.Itoa(int(forwardData.DestPort)))

		pd := client.NewProxyDevice(incus.ProxyDevice{
			Project:  lu.Project,
			Instance: lu.Instance,
			Source:   src,
		})

		dest, err = pd.AddPort()
		if err == nil {
			deviceRegistry.AddDevice(pd)
			defer pd.Shutdown()
		} else {
			log.Errorf("failed to add proxy device for %s (%s): %v", lu, pd, err)
			return
		}
	}

	origDest := net.JoinHostPort(forwardData.DestAddr, strconv.Itoa(int(forwardData.DestPort)))

	log.Debugf("port forwarding to %s for %s (%s:%d => %s)", origDest, lu,
		forwardData.OriginAddr, forwardData.OriginPort, dest)

	var dialer net.Dialer
	dconn, err := dialer.DialContext(ctx, "tcp", dest)
	if err != nil {
		log.Errorf("failed to connect to tcp://%s: %s", dest, err)
		newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("failed to connect to tcp://%s", dest))
		return
	}
	defer dconn.Close()

	ch, reqs, err := newChan.Accept()
	if err != nil {
		log.Errorf("failed to accept new channel request: %s", err)
		return
	}
	defer ch.Close()

	go gossh.DiscardRequests(reqs)

	done := make(chan bool, 2)
	go func() {
		_, err := io.Copy(ch, dconn)
		if err != nil {
			done <- true
		}
		done <- true
	}()
	go func() {
		_, err := io.Copy(dconn, ch)
		if err != nil {
			done <- true
		}
		done <- true
	}()

	<-done
	<-done
	log.Debugf("finished incus port forwarding on %s for %s (%s:%d => %s)", origDest, lu,
		forwardData.OriginAddr, forwardData.OriginPort, dest)
}

func directTCPIPStdioHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	log := log.WithField("session", ctx.ShortSessionID())

	lu := LoginUserFromContext(ctx)
	if !lu.IsValid() {
		log.Errorf("invalid login for %s", lu)
		newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("Invalid login for %q (%s)", lu.OrigUser, lu))
		return
	}

	forwardData := &localForwardChannelData{}
	if err := gossh.Unmarshal(newChan.ExtraData(), forwardData); err != nil {
		log.Errorf("error parsing forward data for %s %s: %s", lu, newChan.ExtraData(), err)
		newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("error parsing forward data: %v", err))
		return
	}

	log.Debugf("direct-tcpip channel data for %s: %s", lu, forwardData)

	destAddr := forwardData.DestAddr
	destPort := forwardData.DestPort
	instanceAddr, ok := ctx.Value(ContextKeyResolvedInstanceAddr).(string)
	if !ok {
		client, err := NewDefaultIncusClientWithContext(ctx)
		if err != nil {
			log.Error(err)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("cannot connect to incus: %v", err))
			return
		}
		defer client.Disconnect()

		networks, err := client.GetInstanceNetworks(lu.Project, lu.Instance)
		network := api.InstanceStateNetwork{}
		for d, v := range networks {
			if strings.HasPrefix(d, "e") {
				network = v
				break
			}
		}

		if len(network.Addresses) == 0 {
			log.Errorf("failed to get instance IP address for %s", lu)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("cannot get instance IP address: %v", err))
			return
		}

		instanceAddr = network.Addresses[0].Address
		ctx.SetValue(ContextKeyResolvedInstanceAddr, instanceAddr)
	}
	log.Debugf("resolved instance address %s for %s", instanceAddr, lu)
	if destAddr == "" && instanceAddr != "" {
		destAddr = instanceAddr
	}

	dest := net.JoinHostPort(destAddr, strconv.Itoa(int(destPort)))

	// if connection is not to instance ip we need to start stdio proxy
	if destAddr != instanceAddr {
		client, err := NewDefaultIncusClientWithContext(ctx)
		if err != nil {
			log.Error(err)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("cannot connect to incus: %v", err))
			return
		}
		defer client.Disconnect()

		err = client.UseProject(lu.Project)
		if err != nil {
			log.Errorf("using project %s error: %s", lu.Project, err)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("cannot connect to incus: %v", err))
			return
		}

		instance, err := client.GetCachedInstance(lu.Project, lu.Instance)
		if err != nil {
			log.Errorf("cannot get instance for %s: %s", lu, err)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("cannot get instance %s: %v", lu.FullInstance(), err))
			return
		}
		//log.Debugf("direct-tcpip: instance: %#v", instance)

		stdioProxyBinBytes, err := stdio_proxy_binary.BinBytes(instance.Architecture)
		if err != nil {
			log.Errorf("failed to get stdio-proxy binary: %s", err)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("failed to get stdio-proxy binary"))
			return
		}
		stdioProxyBinBytes, err = util.Ungz(stdioProxyBinBytes)
		if err != nil {
			log.Errorf("failed to ungzip stdio-proxy: %s", err)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("failed to prepare stdio-proxy"))
			return
		}

		existsParams := &incus.FileExistsParams{
			Project:     lu.Project,
			Instance:    lu.Instance,
			Path:        stdio_proxy_binary.BinName(),
			Md5sum:      util.Md5Bytes(stdioProxyBinBytes),
			ShouldCache: true,
		}
		if !client.FileExists(existsParams) {
			err = client.UploadBytes(lu.Project, lu.Instance, stdio_proxy_binary.BinName(), bytes.NewReader(stdioProxyBinBytes), 0, 0, 0755)
			if err != nil {
				log.Errorf("upload failed: %v", err)
				newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("stdio-proxy is not available on %s\n", lu.FullInstance()))
				return
			}
			log.Debugf("stdio-proxy: uploaded %s to %s", stdio_proxy_binary.BinName(), lu.FullInstance())
		}
		stdioProxyBinBytes = nil

		iu, err := client.GetCachedInstanceUser(lu.Project, lu.Instance, lu.InstanceUser)
		if err != nil {
			log.Errorf("failed to get instance user for %s: %s", lu, err)
			newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("failed to get instance user %s: %v", lu.InstanceUser, err))
			return
		}

		if iu == nil {
			log.Errorf("stdio-proxy: not found instance user for %s", lu)
			newChan.Reject(gossh.ConnectionFailed, "not found user or instance")
			return
		}

		//log.Debugf("stdio-proxy: found instance user %s [%d %d]", iu.User, iu.Uid, iu.Gid)

		ch, reqs, err := newChan.Accept()
		if err != nil {
			log.Errorf("failed to accept new channel request: %s", err)
			return
		}
		defer ch.Close()

		go gossh.DiscardRequests(reqs)

		home := iu.Dir
		uid, gid := iu.Uid, iu.Gid
		log.Debugf("stdio-proxy: starting %s for %s", stdio_proxy_binary.BinName(), lu)

		cmd := fmt.Sprintf("%s tcp:%s:%d", stdio_proxy_binary.BinName(), destAddr, destPort)

		env := make(map[string]string)
		env["USER"] = iu.User
		env["UID"] = fmt.Sprintf("%d", iu.Uid)
		env["GID"] = fmt.Sprintf("%d", iu.Gid)
		env["HOME"] = home

		stdin, stderr, cleanup := util.SetupPipes(ch)
		defer cleanup()

		ie := client.NewInstanceExec(incus.InstanceExec{
			Instance: lu.Instance,
			Cmd:      cmd,
			Env:      env,
			Stdin:    stdin,
			Stdout:   ch,
			Stderr:   stderr,
			User:     uid,
			Group:    gid,
		})

		ret, err := ie.Exec()
		if err != nil {
			if ret != 0 {
				log.Errorf("stdio-proxy exec error: %s", err)
			} else {
				log.Debugf("stdio-proxy exec output: %s", err)
			}
		}

		return
	}

	origDest := net.JoinHostPort(forwardData.DestAddr, strconv.Itoa(int(forwardData.DestPort)))

	log.Debugf("port forwarding to %s for %s (%s:%d => %s)", origDest, lu,
		forwardData.OriginAddr, forwardData.OriginPort, dest)

	var dialer net.Dialer
	dconn, err := dialer.DialContext(ctx, "tcp", dest)
	if err != nil {
		log.Errorf("failed to connect to tcp://%s: %s", dest, err)
		newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("failed to connect to tcp://%s", dest))
		return
	}
	defer dconn.Close()

	ch, reqs, err := newChan.Accept()
	if err != nil {
		log.Errorf("failed to accept new channel request: %s", err)
		return
	}
	defer ch.Close()

	go gossh.DiscardRequests(reqs)

	done := make(chan bool, 2)
	go func() {
		_, err := io.Copy(ch, dconn)
		if err != nil {
			done <- true
		}
		done <- true
	}()
	go func() {
		_, err := io.Copy(dconn, ch)
		if err != nil {
			done <- true
		}
		done <- true
	}()

	<-done
	<-done
	log.Debugf("finished incus port forwarding on %s for %s (%s:%d => %s)", origDest, lu,
		forwardData.OriginAddr, forwardData.OriginPort, dest)
}
