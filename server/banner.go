package server

import (
	"fmt"
	"os"

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
	remote := lu.Remote
	if remote != "" {
		remote += " / "
	}
	hostname, _ := os.Hostname()
	if hostname != "" {
		hostname = fmt.Sprintf(" 💻 %s%s", remote, hostname)
	}
	b := banner + fmt.Sprintf(
		"👤 %s 📦 %s.%s%s\n────────────────────────────────────────────────\n",
		lu.InstanceUser, lu.Instance, lu.Project, hostname,
	)
	return b + "\n"
}
