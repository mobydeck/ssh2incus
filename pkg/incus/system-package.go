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
	env := map[string]string{}

	switch os {
	case "rhel": // RHEL-based (dnf, yum)
		cmd = fmt.Sprintf("dnf install -y %s", pkgs)
	case "debian": // Debian-based (apt)
		cmd = fmt.Sprintf("apt-get update && apt-get install -y %s", pkgs)
		env["DEBIAN_FRONTEND"] = "noninteractive"
	case "alpine": // Alpine (apk)
		cmd = fmt.Sprintf("apk add --no-cache %s", pkgs)
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
	osReleasePath := "/usr/lib/os-release"
	file, err := c.DownloadFile(project, instance, osReleasePath)
	if err != nil {
		return "", fmt.Errorf("failed to download %s: %w", osReleasePath, err)
	}

	// Parse the content as key-value pairs
	osInfo := make(map[string]string)
	for _, lineBytes := range file.Content.Lines() {
		line := string(lineBytes)
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" { // Skip comments and empty lines
			continue
		}
		keyValue := strings.SplitN(line, "=", 2)
		if len(keyValue) != 2 {
			return "", fmt.Errorf("invalid format in %s: %s", osReleasePath, line)
		}
		osInfo[strings.TrimSpace(keyValue[0])] = strings.Trim(keyValue[1], "\"") // Remove quotes from values
	}

	// Determine the OS based on parsed information
	switch osInfo["ID"] {
	case "centos", "rhel": // Add more RHEL-based distros if needed
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
