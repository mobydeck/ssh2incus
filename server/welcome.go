package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ssh2incus/pkg/incus"
)

func welcomeHandler(iu *incus.InstanceUser) string {
	if iu == nil {
		return ""
	}

	paths := []string{"welcome.txt"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".config", "ssh2incus", "welcome.txt"))
	}
	paths = append(paths, filepath.Join("/etc", "ssh2incus", "welcome.txt"))

	replacer := strings.NewReplacer(
		"[INSTANCE_USER]", iu.User,
		"[INSTANCE]", iu.Instance,
		"[PROJECT]", iu.Project,
	)

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		return replacer.Replace(string(data))
	}

	return fmt.Sprintf("Welcome %q to incus shell on %s", iu.User, iu.FullInstance())
}
