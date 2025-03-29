package server

import (
	"context"
	"fmt"

	"ssh2incus/pkg/util/devicereg"

	"github.com/lxc/incus/v6/shared/api"
	log "github.com/sirupsen/logrus"
)

var deviceRegistry *devicereg.DeviceRegistry

func init() {
	deviceRegistry = devicereg.NewDeviceRegistry()
}

func cleanLeftoverProxyDevices() error {
	client, err := NewIncusClientWithContext(context.Background(), DefaultParams)
	if err != nil {
		return err
	}
	defer client.Disconnect()

	allInstances, err := client.GetInstancesAllProjects(api.InstanceTypeAny)
	if err != nil {
		return fmt.Errorf("failed to get instances: %w", err)
	}
	for _, i := range allInstances {
		err = client.UseProject(i.Project)
		if err != nil {
			log.Errorf("use project %s: %v", i.Project, err)
			return err
		}

		for device, _ := range i.Devices {
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
