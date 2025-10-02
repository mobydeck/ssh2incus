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
	"github.com/lxc/incus/v6/shared/cliconfig"
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

type CreateInstanceParams struct {
	Name       string                       `json:"name,omitempty"`
	Project    string                       `json:"project,omitempty"`
	Image      string                       `json:"image,omitempty"`
	Memory     int                          `json:"memory,omitempty"`
	CPU        int                          `json:"cpu,omitempty"`
	Disk       int                          `json:"disk,omitempty"`
	Ephemeral  bool                         `json:"ephemeral,omitempty"`
	Nesting    bool                         `json:"nesting,omitempty"`
	Privileged bool                         `json:"privileged,omitempty"`
	VM         bool                         `json:"vm,omitempty"`
	Config     map[string]string            `json:"config,omitempty"`
	Devices    map[string]map[string]string `json:"devices,omitempty"`
}

func (c *Client) CreateInstance(params CreateInstanceParams) (*api.Instance, error) {
	_, _, err := c.GetInstance(params.Name, params.Project)
	if err == nil {
		return nil, fmt.Errorf("instance %s.%s already exists", params.Name, params.Project)
	}

	err = c.UseProject(params.Project)
	if err != nil {
		return nil, err
	}

	profile, _, err := c.srv.GetProfile("default")
	if err != nil {
		return nil, fmt.Errorf("failed to get default profile: %v", err)
	}

	typ := api.InstanceTypeContainer
	if params.VM {
		typ = api.InstanceTypeVM
	}

	config := params.Config
	if !params.VM {
		if params.Privileged {
			config["security.privileged"] = "true"
		}
		if params.Nesting {
			config["security.nesting"] = "true"
			config["security.syscalls.intercept.mknod"] = "true"
			config["security.syscalls.intercept.setxattr"] = "true"
		}
	}

	if params.Memory > 0 {
		config["limits.memory"] = fmt.Sprintf("%dGiB", params.Memory)
	}

	if params.CPU > 0 {
		config["limits.cpu"] = fmt.Sprintf("%d", params.CPU)
	}

	devices := mergeDevices(profile.Devices, params.Devices)
	if root, ok := devices["root"]; ok {
		if params.Disk > 0 {
			root["size"] = fmt.Sprintf("%dGiB", params.Disk)
		}
		devices["root"] = root
	}

	// Create container configuration
	req := api.InstancesPost{
		Name: params.Name,
		Type: typ,
		Source: api.InstanceSource{
			Type:  "image",
			Alias: params.Image,
		},
		InstancePut: api.InstancePut{
			Config:    config,
			Devices:   devices,
			Ephemeral: params.Ephemeral,
		},
	}

	cc := cliconfig.Config{
		Remotes: map[string]cliconfig.Remote{
			"images": cliconfig.ImagesRemote,
		},
	}

	// Create the container
	var imgServer incus.ImageServer
	var imgInfo *api.Image
	imgServer, err = cc.GetImageServer("images")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to images remote: %v", err)
	}

	imgAlias, _, err := imgServer.GetImageAlias(params.Image)
	if err != nil {
		return nil, fmt.Errorf("failed to get image alias: %v", err)
	}
	imgInfo, _, err = imgServer.GetImage(imgAlias.Target)
	if err != nil {
		return nil, fmt.Errorf("failed to get image info: %v", err)
	}

	rop, err := c.srv.CreateInstanceFromImage(imgServer, *imgInfo, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create instance: %v", err)
	}

	err = rop.Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to create instance: %v", err)
	}

	// Start the instance
	startReq := api.InstanceStatePut{
		Action:   "start",
		Timeout:  -1,
		Force:    false,
		Stateful: false,
	}

	op, err := c.srv.UpdateInstanceState(params.Name, startReq, "")
	if err != nil {
		return nil, fmt.Errorf("failed to start instance: %v", err)
	}

	err = op.Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to start instance: %v", err)
	}

	inst, _, err := c.GetInstance(params.Project, params.Name)
	return inst, err
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

func mergeConfig(c1, c2 map[string]string) map[string]string {
	result := make(map[string]string)

	// Copy all entries from c1
	for k, v := range c1 {
		result[k] = v
	}

	// Merge entries from c2
	for k, v := range c2 {
		result[k] = v
	}

	return result
}

func mergeDevices(d1, d2 map[string]map[string]string) map[string]map[string]string {
	result := make(map[string]map[string]string)

	// Copy all entries from d1
	for k, v := range d1 {
		result[k] = make(map[string]string)
		for innerK, innerV := range v {
			result[k][innerK] = innerV
		}
	}

	// Merge entries from d2
	for k, v := range d2 {
		if _, exists := result[k]; !exists {
			result[k] = make(map[string]string)
		}

		for innerK, innerV := range v {
			result[k][innerK] = innerV
		}
	}

	return result
}
