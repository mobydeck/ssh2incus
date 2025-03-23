package server

import (
	"ssh2incus/pkg/util/ssh"

	log "github.com/sirupsen/logrus"
	gossh "golang.org/x/crypto/ssh"
)

func keyAuthHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	lu := parseUser(ctx.User())

	osUser, err := getOsUser(lu.User)
	if err != nil {
		return false
	}

	if len(config.AllowedGroups) > 0 {
		userGroups, err := getUserGroups(osUser)
		if err != nil {
			return false
		}
		if !isGroupMatch(config.AllowedGroups, userGroups) {
			log.Errorf("auth: no group match for %s in %v", lu.User, userGroups)
			return false
		}
	}

	keys, _ := getUserAuthKeys(osUser)
	for _, k := range keys {
		pk, _, _, _, err := ssh.ParseAuthorizedKey(k)
		if err != nil {
			log.Debugln(err.Error())
			continue
		}
		if ssh.KeysEqual(pk, key) {
			ctx.SetValue("LoginUser", lu)
			log.Debugf("auth succeeded: %s %s key for %#v", key.Type(), gossh.FingerprintSHA256(key), lu)
			return true
		}
	}

	log.Debugf("auth failed: %s %s key for %#v", key.Type(), gossh.FingerprintSHA256(key), lu)
	return false
}

func noAuthHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	lu := parseUser(ctx.User())

	ctx.SetValue("LoginUser", lu)

	return true
}
