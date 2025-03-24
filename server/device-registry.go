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
	server, err := NewIncusServer()
	if err != nil {
		return fmt.Errorf("failed to initialize incus client: %w", err)
	}

	// Connect to Incus
	err = server.Connect(context.Background())
	if err != nil {
		return fmt.Errorf("failed to connect to incus: %w", err)
	}
	defer server.Disconnect()

	allInstances, err := server.GetInstancesAllProjects(api.InstanceTypeAny)
	if err != nil {
		return fmt.Errorf("failed to get instances: %w", err)
	}
	for _, i := range allInstances {
		err = server.UseProject(i.Project)
		if err != nil {
			log.Errorf("use project %s: %v", i.Project, err)
			return err
		}
		instance, etag, err := server.GetInstance(i.Name)
		if err != nil {
			log.Errorf("failed to get instance %s.%s: %v", i.Name, i.Project, err)
			continue
		}

		for device, _ := range i.Devices {
			err = server.DeleteInstanceDevice(instance, device, etag)
			if err != nil {
				log.Errorf("delete instance %s.%s device %s: %v", i.Name, i.Project, deviceRegistry, err)
				continue
			}
			log.Infof("deleted leftover device %s on instance %s.%s", device, i.Name, i.Project)
		}
	}
	return nil
}
