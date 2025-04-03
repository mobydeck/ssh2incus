package server

import (
	"fmt"
	"strings"

	"ssh2incus/pkg/incus"
	"ssh2incus/pkg/ssh"
	"ssh2incus/pkg/util/devicereg"

	"github.com/lxc/incus/v6/shared/api"
	log "github.com/sirupsen/logrus"
)

var deviceRegistry *devicereg.DeviceRegistry

func init() {
	deviceRegistry = devicereg.NewDeviceRegistry()
}

func cleanLeftoverProxyDevices() error {
	ctx, cancel := ssh.NewContext(nil)
	defer cancel()
	client, err := NewDefaultIncusClientWithContext(ctx)
	if err != nil {
		return err
	}
	defer client.Disconnect()

	allInstances, err := client.GetInstancesAllProjects(api.InstanceTypeAny)
	if err != nil {
		return fmt.Errorf("failed to get instances: %w", err)
	}
	for _, i := range allInstances {
		for device := range i.Devices {
			if !strings.HasPrefix(device, incus.ProxyDevicePrefix) {
				continue
			}
			err = client.DeleteInstanceDevice(&i, device)
			if err != nil {
				log.Errorf("delete instance %s.%s device %s: %v", i.Name, i.Project, device, err)
				continue
			}
			log.Infof("deleted leftover device %s on instance %s.%s", device, i.Name, i.Project)
		}
	}
	return nil
}
