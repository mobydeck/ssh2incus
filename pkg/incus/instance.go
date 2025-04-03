package incus

import (
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"ssh2incus/pkg/cache"
	"ssh2incus/pkg/queue"
	"ssh2incus/pkg/util/buffer"

	incus "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

var (
	instanceCache    *cache.Cache
	instanceQueue    *queue.Queueable[*api.InstanceFull]
	instanceInitOnce sync.Once
)

func init() {
	instanceInitOnce.Do(func() {
		instanceCache = cache.New(1*time.Minute, 2*time.Minute)
		instanceQueue = queue.New[*api.InstanceFull](100)
	})
}

func (c *Client) GetInstance(project, name string) (*api.Instance, string, error) {
	err := c.UseProject(project)
	if err != nil {
		return nil, "", err
	}
	return c.srv.GetInstance(name)
}

func (c *Client) GetCachedInstance(project, instance string) (*api.InstanceFull, error) {
	cacheName := fmt.Sprintf("%s/%s", project, instance)
	if in, ok := instanceCache.Get(cacheName); ok {
		return in.(*api.InstanceFull), nil
	}
	in, err := queue.EnqueueWithParam(instanceQueue, func(i string) (*api.InstanceFull, error) {
		full, _, err := c.srv.GetInstanceFull(instance)
		return full, err
	}, instance)
	if err == nil {
		instanceCache.SetDefault(cacheName, in)
	}
	return in, err
}

func (c *Client) GetInstanceMetadata(instance string) (*api.ImageMetadata, string, error) {
	meta, etag, err := c.srv.GetInstanceMetadata(instance)
	return meta, etag, err
}

func (c *Client) GetCachedInstanceState(project, instance string) (*api.InstanceState, error) {
	cacheName := fmt.Sprintf("%s/%s", project, instance)
	if state, ok := instanceStateCache.Get(cacheName); ok {
		return state.(*api.InstanceState), nil
	}
	err := c.UseProject(project)
	if err != nil {
		return nil, err
	}
	state, err := queue.EnqueueWithParam(instanceStateQueue, func(i string) (*api.InstanceState, error) {
		s, _, err := c.srv.GetInstanceState(instance)
		return s, err
	}, instance)
	if err == nil {
		instanceStateCache.SetDefault(cacheName, state)
	}
	return state, err
}

func (c *Client) UpdateInstance(name string, instance api.InstancePut, ETag string) (incus.Operation, error) {
	return c.srv.UpdateInstance(name, instance, ETag)
}

func (c *Client) GetInstancesAllProjects(t api.InstanceType) (instances []api.Instance, err error) {
	return c.srv.GetInstancesAllProjects(t)
}

func (c *Client) GetInstanceNetworks(project, instance string) (map[string]api.InstanceStateNetwork, error) {
	state, err := c.GetCachedInstanceState(project, instance)
	if err != nil {
		return nil, err
	}
	return state.Network, nil
}

func (c *Client) DeleteInstanceDevice(i *api.Instance, name string) error {
	if !strings.HasPrefix(name, ProxyDevicePrefix) {
		return nil
	}

	// Need new ETag for each operation
	in, etag, err := c.srv.GetInstance(i.Name)
	if err != nil {
		return fmt.Errorf("failed to get instance %s.%s: %v", i.Name, i.Project, err)
	}

	device, ok := in.Devices[name]
	if !ok {
		return fmt.Errorf("device %s does not exist for %s.%s", device, in.Name, in.Project)
	}
	delete(in.Devices, name)

	op, err := c.UpdateInstance(in.Name, in.Writable(), etag)
	if err != nil {
		return err
	}

	err = op.Wait()
	if err != nil {
		return err
	}

	// Cleanup socket files
	if strings.HasPrefix(device["connect"], "unix:") {
		source := strings.TrimPrefix(device["connect"], "unix:")
		os.RemoveAll(path.Dir(source))
	}

	if strings.HasPrefix(device["listen"], "unix:") {
		target := strings.TrimPrefix(device["listen"], "unix:")
		cmd := fmt.Sprintf("rm -f %s", target)
		stdout := buffer.NewOutputBuffer()
		stderr := buffer.NewOutputBuffer()
		defer stdout.Close()
		defer stderr.Close()
		ie := c.NewInstanceExec(InstanceExec{
			Instance: in.Name,
			Cmd:      cmd,
			Stdout:   stdout,
			Stderr:   stderr,
		})
		ret, err := ie.Exec()

		if ret != 0 {
			return err
		}
	}

	return nil
}
