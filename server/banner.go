package server

import (
	"fmt"

	"ssh2incus/pkg/util/ssh"
)

var banner = `
--------------------------------------------
         _     ____  _                      
 ___ ___| |__ |___ \(_)_ __   ___ _   _ ___ 
/ __/ __| '_ \  __) | | '_ \ / __| | | / __|
\__ \__ \ | | |/ __/| | | | | (__| |_| \__ \
|___/___/_| |_|_____|_|_| |_|\___|\__,_|___/
--------------------------------------------
`

func bannerHandler(ctx ssh.Context) string {
	lu, ok := ctx.Value("LoginUser").(LoginUser)
	if ok && lu.IsValid() {
		banner += fmt.Sprintf(
			"User %s connected to %s.%s\n--------------------------------------------\n",
			lu.InstanceUser, lu.Instance, lu.Project,
		)
	}
	return banner
}
