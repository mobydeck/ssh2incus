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

func (c *Client) DeleteInstanceDevice(i *api.Instance, name string) error {
	if !strings.HasPrefix(name, ProxyDevicePrefix) {
		return nil
	}

	// Need new ETag for each operation
	i, etag, err := c.srv.GetInstance(i.Name)
	if err != nil {
		return fmt.Errorf("failed to get instance %s.%s: %v", i.Name, i.Project, err)
	}

	device, ok := i.Devices[name]
	if !ok {
		return fmt.Errorf("device %s does not exist for %s.%s", device, i.Name, i.Project)
	}
	delete(i.Devices, name)

	op, err := c.UpdateInstance(i.Name, i.Writable(), etag)
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
			Instance: i.Name,
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
