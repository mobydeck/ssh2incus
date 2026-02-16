package incus

import (
	"fmt"
	"ssh2incus/pkg/util/buffer"
	"strings"
)

func (c *Client) InstallPackages(project, instance string, packages []string) error {
	var cmd string

	// Detect the operating system inside the container and use appropriate package manager
	os, err := c.DetectOS(project, instance)
	if err != nil {
		return fmt.Errorf("failed to detect OS: %w", err)
	}

	pkgs := strings.Join(packages, " ")
	env := map[string]string{
		// "TERM": "xterm256",
	}

	switch os {
	case "rhel": // RHEL-based (dnf, yum)
		cmd = fmt.Sprintf("/bin/sh -c 'dnf install -y %s'", pkgs)
	case "debian": // Debian-based (apt)
		cmd = fmt.Sprintf("/bin/sh -c 'apt-get update && apt-get install -y %s 2>/dev/null'", pkgs)
		// env["DEBIAN_FRONTEND"] = "noninteractive"
	case "alpine": // Alpine (apk)
		cmd = fmt.Sprintf("/bin/sh -c 'apk add --no-cache %s'", pkgs)
	default:
		return fmt.Errorf("unsupported OS: %s", os)
	}

	stdout := buffer.NewOutputBuffer()
	stderr := buffer.NewOutputBuffer()
	ie := c.NewInstanceExec(InstanceExec{
		Instance: instance,
		Cmd:      cmd,
		Env:      env,
		Stdout:   stdout,
		Stderr:   stderr,
	})

	ret, err := ie.Exec()
	if err != nil {
		return fmt.Errorf("package installation failed with error: %w", err)
	}

	if ret != 0 {
		return fmt.Errorf("package installation failed with non-zero exit code: %d", ret)
	}

	return nil
}

func (c *Client) DetectOS(project, instance string) (string, error) {
	osInfo := c.GetOSInfo(project, instance)

	// Determine the OS based on parsed information
	switch osInfo["ID"] {
	case "centos", "rhel", "fedora": // Add more RHEL-based distros if needed
		return "rhel", nil
	case "debian", "ubuntu": // Add more Debian-based distros if needed
		return "debian", nil
	case "alpine": // Add more Alpine-based distros if needed
		return "alpine", nil
	}

	// Check ID_LIKE field for additional matching
	idLikeField := osInfo["ID_LIKE"]
	if idLikeField != "" {
		for _, idLikeEntry := range strings.Split(idLikeField, " ") {
			switch idLikeEntry {
			case "fedora":
				return "rhel", nil
			case "debian": // Add more Debian-based distros if needed
				return "debian", nil
			case "rhel":
				return "rhel", nil
			}
		}
	}

	// If no matching OS was found, return an error
	return "", fmt.Errorf("unsupported or unknown OS")
}
func (c *Client) GuessOSName(project, instance string) (string, error) {
	osInfo := c.GetOSInfo(project, instance)

	if name, ok := osInfo["NAME"]; ok {
		return name, nil
	}

	if id, ok := osInfo["ID"]; ok {
		return id, nil
	}

	if idLike, ok := osInfo["ID_LIKE"]; ok {
		if id, _, ok := strings.Cut(idLike, " "); ok {
			return id, nil
		}
	}

	return "", fmt.Errorf("unknown OS")
}

func (c *Client) GetOSSlug(project, instance string) (string, error) {
	name, err := c.GetOS(project, instance)
	if err != nil {
		return "", err
	}

	slug := strings.ToLower(name)
	return slug, nil
}

func (c *Client) GetOS(project, instance string) (string, error) {
	name, err := c.GuessOSName(project, instance)
	if err != nil {
		return "", err
	}

	slug := strings.ReplaceAll(name, " ", "")
	return slug, nil
}

func (c *Client) GetOSInfo(project, instance string) (osInfo map[string]string) {
	osInfo = make(map[string]string)

	osReleasePaths := []string{
		"/usr/lib/os-release",
		"/etc/os-release",
	}

	var file *InstanceFile
	for _, path := range osReleasePaths {
		file, _ = c.DownloadFile(project, instance, path)
		if file != nil {
			break
		}
	}

	if file == nil {
		return
	}

	// Parse the content as key-value pairs
	for _, lineBytes := range file.Content.Lines() {
		line := string(lineBytes)
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" { // Skip comments and empty lines
			continue
		}
		keyValue := strings.SplitN(line, "=", 2)
		if len(keyValue) != 2 {
			return
		}
		osInfo[strings.TrimSpace(keyValue[0])] = strings.Trim(keyValue[1], "\"") // Remove quotes from values
	}

	return
}
