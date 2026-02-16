package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"ssh2incus/pkg/incus"
	"ssh2incus/pkg/ssh"

	"github.com/labstack/echo/v4"
	incusClient "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
	log "github.com/sirupsen/logrus"
)

// InstanceInfo represents instance information for API responses
type InstanceInfo struct {
	Name            string                 `json:"name"`
	Project         string                 `json:"project"`
	Status          string                 `json:"status"`
	Type            string                 `json:"type"`
	OS              string                 `json:"os"`
	Addresses       []AddressInfo          `json:"addresses"`
	Architecture    string                 `json:"architecture"`
	CreatedAt       string                 `json:"created_at"`
	Ephemeral       bool                   `json:"ephemeral"`
	Stateful        bool                   `json:"stateful"`
	Location        string                 `json:"location"`
	Profiles        []string               `json:"profiles"`
	Config          map[string]string      `json:"config"`
	Devices         map[string]interface{} `json:"devices"`
	ExpandedDevices map[string]interface{} `json:"expanded_devices"`
	ExpandedConfig  map[string]string      `json:"expanded_config"`
	State           interface{}            `json:"state"`
	Metadata        map[string]interface{} `json:"metadata"`
}

// AddressInfo represents an IP address with its network interface
type AddressInfo struct {
	Address   string `json:"address"`
	Interface string `json:"interface"`
}

// InstanceActionRequest represents an action request body
type InstanceActionRequest struct {
	Action   string `json:"action"`   // "start", "stop", "restart"
	Force    bool   `json:"force"`    // Force stop/restart
	Stateful bool   `json:"stateful"` // Store state when stopping
	Delete   bool   `json:"delete"`   // Delete instance after stopping
}

// ConfigSettings represents ssh2incus configuration settings
type ConfigSettings struct {
	Master       bool   `json:"master"`
	Debug        bool   `json:"debug"`
	Banner       bool   `json:"banner"`
	NoAuth       bool   `json:"no_auth"`
	InstanceAuth bool   `json:"instance_auth"`
	PasswordAuth bool   `json:"password_auth"`
	AllowCreate  bool   `json:"allow_create"`
	ChrootSFTP   bool   `json:"chroot_sftp"`
	Web          bool   `json:"web"`
	Welcome      bool   `json:"welcome"`
	Shell        string `json:"shell"`
	Groups       string `json:"groups"`
	TermMux      string `json:"term_mux"`
	HealthCheck  string `json:"health_check"`
	Listen       string `json:"listen"`
	WebListen    string `json:"web_listen"`
	Version      string `json:"version"`
	Edition      string `json:"edition"`
}

// configHandler handles GET /api/config
func (ws *WebServer) configHandler(c echo.Context) error {
	settings := ConfigSettings{
		Master:       config.Master,
		Debug:        config.Debug,
		Banner:       config.Banner,
		NoAuth:       config.NoAuth,
		InstanceAuth: config.InstanceAuth,
		PasswordAuth: config.PassAuth,
		AllowCreate:  config.AllowCreate,
		ChrootSFTP:   config.ChrootSFTP,
		Welcome:      config.Welcome,
		Web:          config.Web,
		Shell:        config.Shell,
		Groups:       config.Groups,
		TermMux:      config.TermMux,
		HealthCheck:  config.HealthCheck,
		Listen:       config.Listen,
		WebListen:    config.WebListen,
		Version:      config.App.Version(),
		Edition:      "community",
	}

	return c.JSON(http.StatusOK, settings)
}

// instanceDetailsHandler handles GET /api/instances/:instance
func (ws *WebServer) instanceDetailsHandler(c echo.Context) error {
	instance := c.Param("instance")
	parts := strings.Split(instance, ".")
	name := parts[0]
	project := "default"
	if len(parts) > 1 {
		project = parts[1]
	}

	ctx, cancel := ssh.NewContext(nil)
	defer cancel()

	client, err := NewDefaultIncusClientWithContext(ctx)
	if err != nil {
		log.Errorf("web: failed to create incus client: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to connect to Incus: %v", err),
		})
	}
	defer client.Disconnect()

	// Get instance server and use the specified project
	projectClient := client.GetInstanceServer().UseProject(project)

	// Get instance details
	inst, _, err := projectClient.GetInstance(name)
	if err != nil {
		log.Errorf("web: failed to get instance %s.%s: %v", name, project, err)
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": fmt.Sprintf("Instance not found: %v", err),
		})
	}

	// Get instance state
	state, _, err := projectClient.GetInstanceState(name)
	if err != nil {
		log.Errorf("web: failed to get state for instance %s.%s: %v", name, project, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to get instance state: %v", err),
		})
	}

	// Get instance metadata
	var metadata map[string]interface{}
	metaData, _, err := projectClient.GetInstanceMetadata(name)
	if err != nil {
		log.Warnf("web: failed to get metadata for instance %s.%s: %v", name, project, err)
		metadata = make(map[string]interface{})
	} else if metaData != nil {
		// Convert metadata to map[string]interface{}
		metadata = make(map[string]interface{})
		if metaData.Properties != nil {
			for k, v := range metaData.Properties {
				metadata[k] = v
			}
		}
	} else {
		metadata = make(map[string]interface{})
	}

	osName := ""
	if imageOS, ok := inst.Config["image.os"]; ok {
		osName = imageOS
	}

	if osName == "" {
		osName, _ = client.GetOS(project, name)
	}

	// Extract IP addresses with interface names and sort consistently
	var addresses []AddressInfo
	if state.Network != nil {
		for ifName, network := range state.Network {
			for _, addr := range network.Addresses {
				if addr.Family == "inet" || addr.Family == "inet6" {
					addresses = append(addresses, AddressInfo{
						Address:   addr.Address,
						Interface: ifName,
					})
				}
			}
		}
	}

	// Sort addresses: eth/en first, then other interfaces, lo last
	sort.Slice(addresses, func(i, j int) bool {
		ifI := addresses[i].Interface
		ifJ := addresses[j].Interface
		addrI := addresses[i].Address
		addrJ := addresses[j].Address

		priorityI := getInterfacePriority(ifI)
		priorityJ := getInterfacePriority(ifJ)

		if priorityI != priorityJ {
			return priorityI < priorityJ
		}

		isIPv4I := !isIPv6(addrI)
		isIPv4J := !isIPv6(addrJ)

		if isIPv4I != isIPv4J {
			return isIPv4I
		}

		return addrI < addrJ
	})

	// Convert config maps to map[string]string
	configStr := make(map[string]string)
	if inst.Config != nil {
		for k, v := range inst.Config {
			configStr[k] = v
		}
	}

	expandedConfigStr := make(map[string]string)
	if inst.ExpandedConfig != nil {
		for k, v := range inst.ExpandedConfig {
			expandedConfigStr[k] = v
		}
	}

	// Convert devices
	devicesMap := make(map[string]interface{})
	if inst.Devices != nil {
		for k, v := range inst.Devices {
			devicesMap[k] = v
		}
	}

	expandedDevicesMap := make(map[string]interface{})
	if inst.ExpandedDevices != nil {
		for k, v := range inst.ExpandedDevices {
			expandedDevicesMap[k] = v
		}
	}

	instanceInfo := InstanceInfo{
		Name:            inst.Name,
		Project:         project,
		Status:          inst.Status,
		Type:            inst.Type,
		OS:              osName,
		Addresses:       addresses,
		Architecture:    inst.Architecture,
		CreatedAt:       inst.CreatedAt.String(),
		Ephemeral:       inst.Ephemeral,
		Stateful:        inst.Stateful,
		Location:        inst.Location,
		Profiles:        inst.Profiles,
		Config:          configStr,
		Devices:         devicesMap,
		ExpandedDevices: expandedDevicesMap,
		ExpandedConfig:  expandedConfigStr,
		State:           state,
		Metadata:        metadata,
	}

	return c.JSON(http.StatusOK, instanceInfo)
}

// listInstancesHandler handles GET /api/instances - fast endpoint for table view
func (ws *WebServer) listInstancesHandler(c echo.Context) error {
	ctx, cancel := ssh.NewContext(nil)
	defer cancel()

	client, err := NewDefaultIncusClientWithContext(ctx)
	if err != nil {
		log.Errorf("web: failed to create incus client: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to connect to Incus: %v", err),
		})
	}
	defer client.Disconnect()

	// Get all projects
	projects, err := client.GetProjects()
	if err != nil {
		log.Errorf("web: failed to get projects: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to get projects: %v", err),
		})
	}

	var instances []InstanceInfo

	// Iterate through projects
	for _, project := range projects {
		projectClient := client.GetInstanceServer().UseProject(project.Name)

		// Get instances in this project
		projectInstances, err := projectClient.GetInstances(api.InstanceTypeAny)
		if err != nil {
			log.Warnf("web: failed to get instances for project %s: %v", project.Name, err)
			continue
		}

		for _, inst := range projectInstances {
			// Only get state for addresses
			state, _, err := projectClient.GetInstanceState(inst.Name)
			if err != nil {
				log.Warnf("web: failed to get state for instance %s: %v", inst.Name, err)
				continue
			}

			// Extract IP addresses with interface names and sort consistently
			var addresses []AddressInfo
			if state.Network != nil {
				for ifName, network := range state.Network {
					for _, addr := range network.Addresses {
						if addr.Family == "inet" || addr.Family == "inet6" {
							addresses = append(addresses, AddressInfo{
								Address:   addr.Address,
								Interface: ifName,
							})
						}
					}
				}
			}

			// Sort addresses: eth/en first, then other interfaces, lo last
			sort.Slice(addresses, func(i, j int) bool {
				ifI := addresses[i].Interface
				ifJ := addresses[j].Interface
				addrI := addresses[i].Address
				addrJ := addresses[j].Address

				priorityI := getInterfacePriority(ifI)
				priorityJ := getInterfacePriority(ifJ)

				if priorityI != priorityJ {
					return priorityI < priorityJ
				}

				isIPv4I := !isIPv6(addrI)
				isIPv4J := !isIPv6(addrJ)

				if isIPv4I != isIPv4J {
					return isIPv4I
				}

				return addrI < addrJ
			})

			// Build minimal config with only image.type and extract image.os
			configStr := make(map[string]string)
			osName := ""
			if inst.Config != nil {
				if imageType, ok := inst.Config["image.type"]; ok {
					configStr["image.type"] = imageType
				}
				if imageOS, ok := inst.Config["image.os"]; ok {
					osName = imageOS
				}
			}

			if osName == "" {
				osName, _ = client.GetOS(project.Name, inst.Name)
			}

			instances = append(instances, InstanceInfo{
				Name:      inst.Name,
				Project:   project.Name,
				Status:    inst.Status,
				Type:      inst.Type,
				OS:        osName,
				Addresses: addresses,
				Config:    configStr,
			})
		}
	}

	return c.JSON(http.StatusOK, instances)
}

// listInstancesDetailedHandler handles GET /api/instances/detailed - detailed endpoint for drawer
func (ws *WebServer) listInstancesDetailedHandler(c echo.Context) error {
	ctx, cancel := ssh.NewContext(nil)
	defer cancel()

	client, err := NewDefaultIncusClientWithContext(ctx)
	if err != nil {
		log.Errorf("web: failed to create incus client: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to connect to Incus: %v", err),
		})
	}
	defer client.Disconnect()

	// Get all projects
	projects, err := client.GetProjects()
	if err != nil {
		log.Errorf("web: failed to get projects: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to get projects: %v", err),
		})
	}

	var instances []InstanceInfo

	// Iterate through projects
	for _, project := range projects {
		projectClient := client.GetInstanceServer().UseProject(project.Name)

		// Get instances in this project
		projectInstances, err := projectClient.GetInstances(api.InstanceTypeAny)
		if err != nil {
			log.Warnf("web: failed to get instances for project %s: %v", project.Name, err)
			continue
		}

		for _, inst := range projectInstances {
			state, _, err := projectClient.GetInstanceState(inst.Name)
			if err != nil {
				log.Warnf("web: failed to get state for instance %s: %v", inst.Name, err)
				continue
			}

			// Get instance metadata
			var metadata map[string]interface{}
			metaData, _, err := projectClient.GetInstanceMetadata(inst.Name)
			if err != nil {
				log.Warnf("web: failed to get metadata for instance %s: %v", inst.Name, err)
				metadata = make(map[string]interface{})
			} else if metaData != nil {
				// Convert metadata to map[string]interface{}
				metadata = make(map[string]interface{})
				if metaData.Properties != nil {
					for k, v := range metaData.Properties {
						metadata[k] = v
					}
				}
			} else {
				metadata = make(map[string]interface{})
			}

			// Extract IP addresses with interface names and sort consistently
			var addresses []AddressInfo
			if state.Network != nil {
				for ifName, network := range state.Network {
					for _, addr := range network.Addresses {
						if addr.Family == "inet" || addr.Family == "inet6" {
							addresses = append(addresses, AddressInfo{
								Address:   addr.Address,
								Interface: ifName,
							})
						}
					}
				}
			}

			// Sort addresses: eth/en first, then other interfaces, lo last
			// Within each group: IPv4 before IPv6, then alphabetically
			sort.Slice(addresses, func(i, j int) bool {
				ifI := addresses[i].Interface
				ifJ := addresses[j].Interface
				addrI := addresses[i].Address
				addrJ := addresses[j].Address

				// Get interface priority
				priorityI := getInterfacePriority(ifI)
				priorityJ := getInterfacePriority(ifJ)

				// Different interface priorities - compare priorities
				if priorityI != priorityJ {
					return priorityI < priorityJ
				}

				// Same interface priority, check IP version
				isIPv4I := !isIPv6(addrI)
				isIPv4J := !isIPv6(addrJ)

				// IPv4 comes before IPv6
				if isIPv4I != isIPv4J {
					return isIPv4I
				}

				// Same family, sort addresses alphabetically
				return addrI < addrJ
			})

			// Convert config maps to map[string]string
			configStr := make(map[string]string)
			if inst.Config != nil {
				for k, v := range inst.Config {
					configStr[k] = v
				}
			}

			expandedConfigStr := make(map[string]string)
			if inst.ExpandedConfig != nil {
				for k, v := range inst.ExpandedConfig {
					expandedConfigStr[k] = v
				}
			}

			// Convert DevicesMap to map[string]interface{}
			devicesInterface := make(map[string]interface{})
			for k, v := range inst.Devices {
				devicesInterface[k] = v
			}

			expandedDevicesInterface := make(map[string]interface{})
			for k, v := range inst.ExpandedDevices {
				expandedDevicesInterface[k] = v
			}

			instances = append(instances, InstanceInfo{
				Name:            inst.Name,
				Project:         project.Name,
				Status:          inst.Status,
				Type:            inst.Type,
				Addresses:       addresses,
				Architecture:    inst.Architecture,
				CreatedAt:       inst.CreatedAt.String(),
				Ephemeral:       inst.Ephemeral,
				Stateful:        inst.Stateful,
				Location:        inst.Location,
				Profiles:        inst.Profiles,
				Config:          configStr,
				Devices:         devicesInterface,
				ExpandedDevices: expandedDevicesInterface,
				ExpandedConfig:  expandedConfigStr,
				State:           state,
				Metadata:        metadata,
			})
		}
	}

	return c.JSON(http.StatusOK, instances)
}

// instanceActionHandler handles POST /api/instances/:instance/action
func (ws *WebServer) instanceActionHandler(c echo.Context) error {
	instance := c.Param("instance")
	parts := strings.Split(instance, ".")
	name := parts[0]
	project := "default"
	if len(parts) > 1 {
		project = parts[1]
	}

	var req InstanceActionRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid request body",
		})
	}

	// Validate action
	validActions := map[string]bool{
		"start":   true,
		"stop":    true,
		"restart": true,
	}
	if !validActions[req.Action] {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid action. Must be start, stop, or restart",
		})
	}

	ctx, cancel := ssh.NewContext(nil)
	defer cancel()

	client, err := NewDefaultIncusClientWithContext(ctx)
	if err != nil {
		log.Errorf("web: failed to create incus client: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to connect to Incus: %v", err),
		})
	}
	defer client.Disconnect()

	// Get instance server and use the specified project
	projectClient := client.GetInstanceServer().UseProject(project)

	// Execute action with timeout
	actionCtx, actionCancel := context.WithTimeout(ctx, 30*time.Second)
	defer actionCancel()

	statePut := api.InstanceStatePut{
		Timeout: -1,
		Force:   req.Force,
	}

	var op incusClient.Operation
	switch req.Action {
	case "start":
		statePut.Action = "start"
		op, err = projectClient.UpdateInstanceState(name, statePut, "")
	case "stop":
		statePut.Action = "stop"
		if req.Stateful {
			statePut.Stateful = true
		}
		op, err = projectClient.UpdateInstanceState(name, statePut, "")
	case "restart":
		statePut.Action = "restart"
		op, err = projectClient.UpdateInstanceState(name, statePut, "")
	}

	if err != nil {
		log.Errorf("web: failed to execute action %s on %s.%s: %v", req.Action, name, project, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to execute action: %v", err),
		})
	}

	// Wait for operation to complete
	err = op.WaitContext(actionCtx)
	if err != nil {
		log.Errorf("web: action %s on %s.%s failed: %v", req.Action, name, project, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Action failed: %v", err),
		})
	}

	// If delete is requested, delete the instance after stopping
	if req.Action == "stop" && req.Delete {
		deleteOp, err := projectClient.DeleteInstance(name)
		if err != nil {
			log.Errorf("web: failed to delete instance %s.%s: %v", name, project, err)
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("Failed to delete instance: %v", err),
			})
		}

		// Wait for delete operation to complete
		deleteCtx, deleteCancel := context.WithTimeout(ctx, 30*time.Second)
		defer deleteCancel()
		err = deleteOp.WaitContext(deleteCtx)
		if err != nil {
			log.Errorf("web: delete instance %s.%s failed: %v", name, project, err)
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("Failed to delete instance: %v", err),
			})
		}
	}

	return c.JSON(http.StatusOK, map[string]string{
		"status": "success",
		"action": req.Action,
	})
}

// getInterfacePriority returns priority for interface ordering
// Lower number = higher priority (appears first)
// Priority: eth/en interfaces (0), other interfaces (1), lo interface (2)
func getInterfacePriority(iface string) int {
	if iface == "lo" || iface == "lo0" {
		return 2 // lo interface last
	}
	if len(iface) >= 2 {
		prefix := iface[:2]
		if prefix == "et" || prefix == "en" {
			return 0 // eth/en first
		}
	}
	return 1 // other interfaces in middle
}

// isIPv6 checks if an IP address string is IPv6 (contains colons)
func isIPv6(ip string) bool {
	// IPv6 addresses contain colons, IPv4 addresses don't
	for _, c := range ip {
		if c == ':' {
			return true
		}
	}
	return false
}

// profilesHandler handles GET /api/profiles
func (ws *WebServer) profilesHandler(c echo.Context) error {
	// Load create-config if available
	createConfig, err := LoadCreateConfigWithFallback([]string{
		"./",
		os.ExpandEnv("$HOME/.config/ssh2incus"),
		"/etc/ssh2incus",
	})
	if err != nil {
		log.Warnf("web: failed to load create-config: %v", err)
		return c.JSON(http.StatusOK, map[string]interface{}{
			"profiles": []interface{}{},
		})
	}

	// Build response with all profiles
	profiles := make([]map[string]interface{}, 0)

	// Add defa   ult profile
	defaultProf := map[string]interface{}{
		"name":      "default",
		"image":     createConfig.Image(),
		"memory":    createConfig.Memory(),
		"cpu":       createConfig.CPU(),
		"disk":      createConfig.Disk(),
		"ephemeral": createConfig.Ephemeral(),
		"config":    createConfig.Config(),
		"devices":   createConfig.Devices(),
	}
	profiles = append(profiles, defaultProf)

	// Add custom profiles
	for profileName, profileConfig := range createConfig.GetProfiles() {
		if profileName == "default" {
			continue // Skip default, already added
		}
		prof := map[string]interface{}{
			"name": profileName,
		}
		if profileConfig.Image != nil {
			prof["image"] = *profileConfig.Image
		}
		if profileConfig.Memory != nil {
			prof["memory"] = *profileConfig.Memory
		}
		if profileConfig.CPU != nil {
			prof["cpu"] = *profileConfig.CPU
		}
		if profileConfig.Disk != nil {
			prof["disk"] = *profileConfig.Disk
		}
		if profileConfig.Ephemeral != nil {
			prof["ephemeral"] = *profileConfig.Ephemeral
		}
		if profileConfig.Config != nil {
			prof["config"] = profileConfig.Config
		}
		if profileConfig.Devices != nil {
			prof["devices"] = profileConfig.Devices
		}
		profiles = append(profiles, prof)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"profiles": profiles,
	})
}

// imagesHandler handles GET /api/images
func (ws *WebServer) imagesHandler(c echo.Context) error {
	remote := c.QueryParam("remote")
	if remote == "" {
		remote = "images"
	}

	// remotes := []string{"", remote}
	// remotes := []string{""}

	// Get Incus client
	ctx, cancel := ssh.NewContext(nil)
	defer cancel()

	client, err := NewDefaultIncusClientWithContext(ctx)
	if err != nil {
		log.Errorf("web: failed to create incus client: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to connect to Incus",
		})
	}
	defer client.Disconnect()

	var imageList []map[string]interface{}
	localImages, _ := client.GetInstanceServer().GetImages()
	remoteImages, _ := client.ListImages(remote)

	remotes := make(map[string][]api.Image)
	remotes["local"] = localImages
	remotes[remote] = remoteImages

	// List images
	// imageList := make([]map[string]interface{}, 0, len(images))
	// for _, remote := range remotes {
	// images, err := client.ListImages(remote)
	// if err != nil {
	// 	log.Errorf("web: failed to list images: %v", err)
	// 	return c.JSON(http.StatusInternalServerError, map[string]string{
	// 		"error": fmt.Sprintf("Failed to list images: %v", err),
	// 	})
	// }
	for remote, images := range remotes {
		// Build response
		for _, img := range images {
			// Extract alias names from ImageAlias structs
			aliasNames := make([]string, 0, len(img.Aliases))
			for _, alias := range img.Aliases {
				aliasNames = append(aliasNames, alias.Name)
			}

			imgData := map[string]interface{}{
				"fingerprint":  img.Fingerprint,
				"aliases":      aliasNames,
				"architecture": img.Architecture,
				"properties":   img.Properties,
				"type":         img.Type,
				"remote":       remote,
			}
			imageList = append(imageList, imgData)
		}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"images": imageList,
	})
}

// projectsHandler handles GET /api/projects
func (ws *WebServer) projectsHandler(c echo.Context) error {
	ctx, cancel := ssh.NewContext(nil)
	defer cancel()

	client, err := NewDefaultIncusClientWithContext(ctx)
	if err != nil {
		log.Errorf("web: failed to create incus client: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to connect to Incus",
		})
	}
	defer client.Disconnect()

	projects, err := client.ListProjects()
	if err != nil {
		log.Errorf("web: failed to list projects: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to list projects: %v", err),
		})
	}

	// Build response
	projList := make([]map[string]interface{}, 0, len(projects))
	for _, p := range projects {
		projData := map[string]interface{}{
			"name":        p.Name,
			"description": p.Description,
		}
		projList = append(projList, projData)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"projects": projList,
	})
}

// instanceExistsHandler handles GET /api/instances/:instance/exists
func (ws *WebServer) instanceExistsHandler(c echo.Context) error {
	instance := c.Param("instance")

	// Parse instance parameter in format "name.project"
	parts := strings.Split(instance, ".")
	name := parts[0]
	project := "default"
	if len(parts) > 1 {
		project = parts[1]
	}

	ctx, cancel := ssh.NewContext(nil)
	defer cancel()

	client, err := NewDefaultIncusClientWithContext(ctx)
	if err != nil {
		log.Errorf("web: failed to create incus client: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to connect to Incus",
		})
	}
	defer client.Disconnect()

	exists, err := client.InstanceExists(name, project)
	if err != nil {
		log.Errorf("web: failed to check instance existence: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to check instance: %v", err),
		})
	}

	return c.JSON(http.StatusOK, map[string]bool{
		"exists": exists,
	})
}

// CreateInstanceRequest represents the request body for instance creation
type CreateInstanceRequest struct {
	Name      string                       `json:"name"`
	Project   string                       `json:"project"`
	Image     string                       `json:"image"`
	Memory    int                          `json:"memory"`
	CPU       int                          `json:"cpu"`
	Disk      int                          `json:"disk"`
	Ephemeral bool                         `json:"ephemeral"`
	Config    map[string]string            `json:"config"`
	Devices   map[string]map[string]string `json:"devices"`
	InitOnly  bool                         `json:"init_only"`
	Profile   string                       `json:"profile"`
}

// createInstanceHandler handles POST /api/instances
func (ws *WebServer) createInstanceHandler(c echo.Context) error {
	var req CreateInstanceRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid request body",
		})
	}

	// Validate required fields
	if req.Name == "" || req.Project == "" || req.Image == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "name, project, and image are required",
		})
	}

	ctx, cancel := ssh.NewContext(nil)
	defer cancel()

	client, err := NewDefaultIncusClientWithContext(ctx)
	if err != nil {
		log.Errorf("web: failed to create incus client: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to connect to Incus",
		})
	}
	defer client.Disconnect()

	// Check if instance already exists
	exists, err := client.InstanceExists(req.Name, req.Project)
	if err != nil {
		log.Errorf("web: failed to check instance existence: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to check instance",
		})
	}
	if exists {
		return c.JSON(http.StatusConflict, map[string]string{
			"error": fmt.Sprintf("instance %s already exists in project %s", req.Name, req.Project),
		})
	}

	// var config map[string]string
	// var devices map[string]map[string]string

	// // Load create-config for profile merging
	// createConfig, err := LoadCreateConfigWithFallback([]string{
	// 	"./",
	// 	os.ExpandEnv("$HOME/.config/ssh2incus"),
	// 	"/etc/ssh2incus",
	// })

	// if err == nil && req.Profile != "" && req.Profile != "default" {
	// 	// Merge profile with defaults
	// 	merged, err := createConfig.MergeProfiles([]string{req.Profile})
	// 	if err != nil {
	// 		return c.JSON(http.StatusBadRequest, map[string]string{
	// 			"error": fmt.Sprintf("Invalid profile: %v", err),
	// 		})
	// 	}

	// 	// Start with merged config
	// 	config = make(map[string]string)
	// 	if merged.Config != nil {
	// 		for k, v := range merged.Config {
	// 			config[k] = v
	// 		}
	// 	}
	// 	devices = make(map[string]map[string]string)
	// 	if merged.Devices != nil {
	// 		for k, v := range merged.Devices {
	// 			devices[k] = v
	// 		}
	// 	}
	// } else {
	// 	config = make(map[string]string)
	// 	devices = make(map[string]map[string]string)
	// }

	config := make(map[string]string)
	devices := make(map[string]map[string]string)

	// Merge user-provided config and devices
	if req.Config != nil {
		for k, v := range req.Config {
			config[k] = v
		}
	}
	if req.Devices != nil {
		for k, v := range req.Devices {
			devices[k] = v
		}
	}

	remote := ""
	image := req.Image
	if r, i, found := strings.Cut(req.Image, ":"); found {
		remote = r
		image = i
	}

	// Create instance
	params := incus.CreateInstanceParams{
		Name:        req.Name,
		Project:     req.Project,
		Image:       image,
		ImageRemote: remote,
		Memory:      req.Memory,
		CPU:         req.CPU,
		Disk:        req.Disk,
		Ephemeral:   req.Ephemeral,
		InitOnly:    req.InitOnly,
		Config:      config,
		Devices:     devices,
	}

	inst, err := client.CreateInstance(params)
	if err != nil {
		log.Errorf("web: failed to create instance: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to create instance: %v", err),
		})
	}

	// Build instance info response
	instInfo := InstanceInfo{
		Name:    inst.Name,
		Project: inst.Project,
		Status:  inst.Status,
		Type:    string(inst.Type),
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"status":   "success",
		"instance": instInfo,
	})
}
