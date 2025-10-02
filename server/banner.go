package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ssh2incus/pkg/ssh"
)

const banner = `
┌──────────────────────────────────────────────┐
│          _     ____  _                       │
│  ___ ___| |__ |___ \(_)_ __   ___ _   _ ___  │
│ / __/ __| '_ \  __) | | '_ \ / __| | | / __| │
│ \__ \__ \ | | |/ __/| | | | | (__| |_| \__ \ │
│ |___/___/_| |_|_____|_|_| |_|\___|\__,_|___/ │
└──────────────────────────────────────────────┘
`

func bannerHandler(ctx ssh.Context) string {
	lu := LoginUserFromContext(ctx)
	if !lu.IsValid() {
		return ""
	}
	if lu.IsCommand() {
		return banner
	}

	hostname, _ := os.Hostname()
	displayHostname := hostname

	remote := lu.Remote
	if remote != "" {
		remote += " / "
	}
	if displayHostname != "" {
		displayHostname = fmt.Sprintf(" 💻 %s%s", remote, displayHostname)
	}

	paths := []string{"banner.txt"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".config", "ssh2incus", "banner.txt"))
	}
	paths = append(paths, filepath.Join("/etc", "ssh2incus", "banner.txt"))

	replacer := strings.NewReplacer(
		"[INSTANCE_USER]", lu.InstanceUser,
		"[INSTANCE]", lu.Instance,
		"[PROJECT]", lu.Project,
		"[REMOTE]", lu.Remote,
		"[HOSTNAME]", hostname,
	)

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return "\n" + replacer.Replace(string(data)) + "\n"
	}

	b := banner + fmt.Sprintf(
		"👤 %s 📦 %s.%s%s\n────────────────────────────────────────────────\n",
		lu.InstanceUser, lu.Instance, lu.Project, displayHostname,
	)
	return b + "\n"
}
