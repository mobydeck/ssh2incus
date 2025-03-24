package server

import (
	"ssh2incus/pkg/util/devicereg"
)

var deviceRegistry *devicereg.DeviceRegistry

func init() {
	deviceRegistry = devicereg.NewDeviceRegistry()
}
