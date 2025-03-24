package incus

import (
	"context"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"ssh2incus/pkg/util"
	"ssh2incus/pkg/util/buffer"
)

const (
	ProxyDeviceSocket = "socket"
	ProxyDevicePort   = "port"

	ProxyDevicePrefix = "ssh2incus-proxy"
)

type ProxyDevice struct {
	srv *Server

	Project  string
	Instance string
	Source   string
	Uid      int
	Gid      int
	Mode     string

	deviceName string
	target     string
	typ        string
}

func (s *Server) NewProxyDevice(d ProxyDevice) *ProxyDevice {
	return &ProxyDevice{
		srv:      s,
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

func (p *ProxyDevice) Shutdown() error {
	switch p.typ {
	case ProxyDeviceSocket:
		p.RemoveSocket()
	case ProxyDevicePort:
		p.RemovePort()
	}
	return nil
}

func (p *ProxyDevice) AddSocket() (string, error) {
	p.typ = ProxyDeviceSocket
	instance, etag, err := p.srv.srv.GetInstance(p.Instance)
	if err != nil {
		return "", err
	}

	tmpDir := "/tmp"
	p.deviceName = fmt.Sprintf("%s-socket-%s", ProxyDevicePrefix, strconv.FormatInt(time.Now().UnixNano(), 16)+util.RandomStringLower(5))
	p.target = path.Join(tmpDir, p.deviceName+".sock")

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
	op, err := p.srv.srv.UpdateInstance(instance.Name, instance.Writable(), etag)
	if err != nil {
		return "", err
	}

	err = op.Wait()
	if err != nil {
		return "", err
	}

	return p.target, nil
}

func (p *ProxyDevice) RemoveSocket() error {
	err := p.srv.Connect(context.Background())
	if err != nil {
		return err
	}
	defer p.srv.Disconnect()
	instance, etag, err := p.srv.srv.GetInstance(p.Instance)
	if err != nil {
		return err
	}

	device, ok := instance.Devices[p.deviceName]
	if !ok {
		return fmt.Errorf("device %s does not exist for %s", p.deviceName, instance.Name)
	}
	delete(instance.Devices, p.deviceName)

	op, err := p.srv.srv.UpdateInstance(instance.Name, instance.Writable(), etag)
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
	ie := p.srv.NewInstanceExec(InstanceExec{
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
}

func (p *ProxyDevice) AddPort() (string, error) {
	p.typ = ProxyDevicePort
	instance, etag, err := p.srv.GetInstance(p.Instance)
	if err != nil {
		return "", err
	}

	port, err := util.GetFreePort()
	if err != nil {
		return "", err
	}

	p.deviceName = fmt.Sprintf("%s-port-%d", ProxyDevicePrefix, port)
	p.target = fmt.Sprintf("%d", port)

	_, ok := instance.Devices[p.deviceName]
	if ok {

		return "", fmt.Errorf("device %s already exists for %s", p.deviceName, instance.Name)
	}

	device := map[string]string{}
	device["type"] = "proxy"
	device["connect"] = "tcp:127.0.0.1:" + p.Source
	device["listen"] = "tcp:127.0.0.1:" + p.target
	device["bind"] = "host"

	instance.Devices[p.deviceName] = device
	op, err := p.srv.UpdateInstance(instance.Name, instance.Writable(), etag)
	if err != nil {
		return "", err
	}

	err = op.Wait()
	if err != nil {
		return "", err
	}

	return p.target, nil
}

func (p *ProxyDevice) RemovePort() error {
	err := p.srv.Connect(context.Background())
	if err != nil {
		return err
	}
	defer p.srv.Disconnect()
	instance, etag, err := p.srv.GetInstance(p.Instance)
	if err != nil {
		return err
	}

	_, ok := instance.Devices[p.deviceName]
	if !ok {
		return fmt.Errorf("device %s does not exist for %s", p.deviceName, instance.Name)
	}
	delete(instance.Devices, p.deviceName)

	op, err := p.srv.UpdateInstance(instance.Name, instance.Writable(), etag)
	if err != nil {
		return err
	}

	err = op.Wait()
	if err != nil {
		return err
	}

	return nil
}
