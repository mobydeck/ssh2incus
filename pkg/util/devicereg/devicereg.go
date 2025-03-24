package devicereg

import (
	"context"
	"sync"
)

// Device represents any resource that needs cleanup
type Device interface {
	ID() string
	Shutdown() error
}

// DeviceRegistry keeps track of all devices and handles graceful shutdown
type DeviceRegistry struct {
	devices map[string]Device
	mu      sync.RWMutex
}

// NewDeviceRegistry creates a new device registry
func NewDeviceRegistry() *DeviceRegistry {
	return &DeviceRegistry{
		devices: make(map[string]Device),
	}
}

// AddDevice adds a device to the registry
func (r *DeviceRegistry) AddDevice(device Device) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.devices[device.ID()] = device
}

// RemoveDevice removes a device from the registry
func (r *DeviceRegistry) RemoveDevice(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.devices[id]; exists {
		delete(r.devices, id)
	}
}

// ShutdownAllDevices gracefully shuts down all devices
func (r *DeviceRegistry) ShutdownAllDevices(ctx context.Context) error {
	r.mu.RLock()
	// Create a copy of device IDs to avoid holding the lock during shutdown
	deviceIDs := make([]string, 0, len(r.devices))
	for id := range r.devices {
		deviceIDs = append(deviceIDs, id)
	}
	r.mu.RUnlock()

	// Process each device one by one
	for _, id := range deviceIDs {
		// Check if context is canceled during shutdown
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Get the device (with read lock)
			r.mu.RLock()
			device, exists := r.devices[id]
			r.mu.RUnlock()

			if exists {
				if err := device.Shutdown(); err != nil {
				}
			}
		}
	}

	return nil
}

// Count returns the number of devices in the registry
func (r *DeviceRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.devices)
}
