package server

import (
	"ssh2incus/pkg/ssh"

	log "github.com/sirupsen/logrus"
	gossh "golang.org/x/crypto/ssh"
)

func keyAuthHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	lu := parseLoginUser(ctx.User())
	lu.PublicKey = key

	osUser, err := getOsUser(lu.User)
	if err != nil {
		log.Errorf("auth: %s", err)
		return false
	}

	if osUser.Uid != "0" && len(config.AllowedGroups) > 0 {
		userGroups, err := getUserGroups(osUser)
		if err != nil {
			log.Errorf("auth: %s", err)
			return false
		}
		if !isGroupMatch(config.AllowedGroups, userGroups) {
			log.Warnf("auth: no group match for %s in %v", lu.User, userGroups)
			return false
		}
	}

	keys, err := getUserAuthKeys(osUser)
	if err != nil {
		log.Errorf("auth: %s", err)
		return false
	}

	if len(keys) == 0 {
		log.Warnf("auth: no keys for %s", lu.User)
	}

	for _, k := range keys {
		pkey, _, _, _, err := ssh.ParseAuthorizedKey(k)
		if err != nil {
			log.Errorf("auth: %s", err)
			continue
		}
		if ssh.KeysEqual(pkey, key) {
			ctx.SetValue(ContextKeyLoginUser, lu)
			log.Infof("auth: succeeded %s %s key for %s", key.Type(), gossh.FingerprintSHA256(key), lu)
			return true
		}
	}

	log.Warnf("auth: failed %s %s key for %s", key.Type(), gossh.FingerprintSHA256(key), lu)
	return false
}

func noAuthHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	lu := parseLoginUser(ctx.User())
	lu.PublicKey = key
	ctx.SetValue(ContextKeyLoginUser, lu)
	return true
}
