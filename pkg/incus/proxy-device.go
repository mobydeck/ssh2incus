package incus

import (
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"ssh2incus/pkg/queue"
	"ssh2incus/pkg/util"
	"ssh2incus/pkg/util/buffer"
)

const (
	ProxyDeviceSocket = "socket"
	ProxyDevicePort   = "port"
)

var ProxyDevicePrefix = "proxy"

var (
	proxyDeviceQueue *queue.Queueable[string]
)

func init() {
	proxyDeviceQueue = queue.New[string](100)
}

type ProxyDevice struct {
	client *Client

	Project  string
	Instance string
	Source   string
	Uid      int
	Gid      int
	Mode     string

	deviceName string
	target     string
	typ        string
	listener   net.Listener
}

func (c *Client) NewProxyDevice(d ProxyDevice) *ProxyDevice {
	return &ProxyDevice{
		client:   c,
		Project:  d.Project,
		Instance: d.Instance,
		Source:   d.Source,
		Uid:      d.Uid,
		Gid:      d.Gid,
		Mode:     d.Mode,
	}
}

func (p *ProxyDevice) ID() string {
	return fmt.Sprintf("%s/%s/%s", p.Project, p.Instance, p.deviceName)
}

func (p *ProxyDevice) String() string {
	return fmt.Sprintf("%s => %s", p.Source, p.target)
}

func (p *ProxyDevice) Listener() net.Listener {
	return p.listener
}

func (p *ProxyDevice) Shutdown() error {
	switch p.typ {
	case ProxyDeviceSocket:
		return p.RemoveSocket()
	case ProxyDevicePort:
		return p.RemovePort()
	}
	return nil
}

func (p *ProxyDevice) AddSocket() (string, error) {
	return proxyDeviceQueue.Enqueue(func() (string, error) {
		p.typ = ProxyDeviceSocket

		tmpDir := "/tmp"
		p.deviceName = fmt.Sprintf("%s-socket-%s", ProxyDevicePrefix, strconv.FormatInt(time.Now().UnixNano(), 16)+util.RandomStringLower(5))
		p.target = path.Join(tmpDir, p.deviceName+".sock")

		instance, etag, err := p.client.srv.GetInstance(p.Instance)
		if err != nil {
			return "", err
		}

		_, ok := instance.Devices[p.deviceName]
		if ok {
			return "", fmt.Errorf("device %s already exists for %s.%s", p.deviceName, instance.Name, instance.Project)
		}

		device := map[string]string{}
		device["type"] = "proxy"
		device["connect"] = "unix:" + p.Source
		device["listen"] = "unix:" + p.target
		device["bind"] = "instance"
		device["mode"] = p.Mode
		device["uid"] = strconv.Itoa(p.Uid)
		device["gid"] = strconv.Itoa(p.Gid)

		instance.Devices[p.deviceName] = device
		op, err := p.client.srv.UpdateInstance(instance.Name, instance.Writable(), etag)
		if err != nil {
			return "", err
		}

		err = op.Wait()
		if err != nil {
			return "", err
		}

		return p.target, nil
	})
}

func (p *ProxyDevice) RemoveSocket() error {
	return proxyDeviceQueue.EnqueueError(func() error {
		err := p.client.Connect(context.Background())
		if err != nil {
			return err
		}
		defer p.client.Disconnect()
		instance, etag, err := p.client.srv.GetInstance(p.Instance)
		if err != nil {
			return err
		}

		device, ok := instance.Devices[p.deviceName]
		if !ok {
			return fmt.Errorf("device %s does not exist for %s", p.deviceName, instance.Name)
		}
		delete(instance.Devices, p.deviceName)

		op, err := p.client.srv.UpdateInstance(instance.Name, instance.Writable(), etag)
		if err != nil {
			return err
		}

		err = op.Wait()
		if err != nil {
			return err
		}

		source := strings.TrimPrefix(device["connect"], "unix:")
		os.RemoveAll(path.Dir(source))

		target := strings.TrimPrefix(device["listen"], "unix:")
		cmd := fmt.Sprintf("rm -f %s", target)
		stdout := buffer.NewOutputBuffer()
		stderr := buffer.NewOutputBuffer()
		ie := p.client.NewInstanceExec(InstanceExec{
			Instance: instance.Name,
			Cmd:      cmd,
			Stdout:   stdout,
			Stderr:   stderr,
		})
		ret, err := ie.Exec()

		if ret != 0 {
			return err
		}

		return nil
	})
}

func (p *ProxyDevice) AddPort() (string, error) {
	return proxyDeviceQueue.Enqueue(func() (string, error) {
		p.typ = ProxyDevicePort

		if err := p.fixSource(); err != nil {
			return "", err
		}

		port, err := util.GetFreePort()
		if err != nil {
			return "", err
		}

		p.deviceName = fmt.Sprintf("%s-port-%d", ProxyDevicePrefix, port)
		p.target = fmt.Sprintf("127.0.0.1:%d", port)

		instance, etag, err := p.client.GetInstance(p.Project, p.Instance)
		if err != nil {
			return "", err
		}
		_, ok := instance.Devices[p.deviceName]
		if ok {
			return "", fmt.Errorf("device %s already exists for %s", p.deviceName, instance.Name)
		}

		device := map[string]string{}
		device["type"] = "proxy"
		device["connect"] = "tcp:" + p.Source
		device["listen"] = "tcp:" + p.target
		device["bind"] = "host"

		instance.Devices[p.deviceName] = device
		op, err := p.client.UpdateInstance(instance.Name, instance.Writable(), etag)
		if err != nil {
			return "", err
		}

		err = op.Wait()
		if err != nil {
			return "", err
		}

		return p.target, nil
	})
}

func (p *ProxyDevice) AddReversePort() (string, error) {
	return proxyDeviceQueue.Enqueue(func() (string, error) {
		p.typ = ProxyDevicePort

		if err := p.fixSource(); err != nil {
			return "", err
		}

		addr := net.JoinHostPort("127.0.0.1", "0")

		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return "", fmt.Errorf("error listening on %s", addr)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		p.listener = ln

		p.deviceName = fmt.Sprintf("%s-reverse-port-%d", ProxyDevicePrefix, port)
		p.target = fmt.Sprintf("127.0.0.1:%d", port)

		instance, etag, err := p.client.GetInstance(p.Project, p.Instance)
		if err != nil {
			return "", err
		}
		_, ok := instance.Devices[p.deviceName]
		if ok {
			return "", fmt.Errorf("device %s already exists for %s", p.deviceName, instance.Name)
		}

		device := map[string]string{}
		device["type"] = "proxy"
		device["connect"] = "tcp:" + p.target
		device["listen"] = "tcp:" + p.Source
		device["bind"] = "instance"

		instance.Devices[p.deviceName] = device
		op, err := p.client.UpdateInstance(instance.Name, instance.Writable(), etag)
		if err != nil {
			return "", err
		}

		err = op.Wait()
		if err != nil {
			return "", err
		}

		return p.target, nil
	})

}

func (p *ProxyDevice) RemovePort() error {
	return proxyDeviceQueue.EnqueueError(func() error {
		err := p.client.Connect(context.Background())
		if err != nil {
			return err
		}
		defer p.client.Disconnect()
		if p.listener != nil {
			defer p.listener.Close()
		}

		if p.Instance == "" {
			return fmt.Errorf("instance name is empty")
		}

		instance, etag, err := p.client.GetInstance(p.Project, p.Instance)
		if err != nil {
			return err
		}

		_, ok := instance.Devices[p.deviceName]
		if !ok {
			return fmt.Errorf("device %s does not exist for %s", p.deviceName, instance.Name)
		}

		delete(instance.Devices, p.deviceName)

		op, err := p.client.UpdateInstance(instance.Name, instance.Writable(), etag)
		if err != nil {
			return err
		}

		err = op.Wait()
		if err != nil {
			return err
		}

		return nil
	})
}

func (p *ProxyDevice) fixSource() error {
	if !strings.Contains(p.Source, ":") {
		p.Source = fmt.Sprintf("127.0.0.1:%s", p.Source)
	}

	sourceAddr, sourcePort, err := net.SplitHostPort(p.Source)
	if err != nil {
		return err
	}

	if !util.IsIPAddress(sourceAddr) {
		ips, err := util.NewDNSResolver().LookupHost(sourceAddr)
		if err != nil {
			return err
		}
		if len(ips) == 0 {
			return fmt.Errorf("no IP address found for %s", sourceAddr)
		}
		sourceAddr = ips[0].String()
	}

	p.Source = net.JoinHostPort(sourceAddr, sourcePort)
	return nil
}
