package server

import (
	"ssh2incus/pkg/ssh"
	"ssh2incus/pkg/user"

	log "github.com/sirupsen/logrus"
	gossh "golang.org/x/crypto/ssh"
)

func hostAuthHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	log := log.WithField("session", ctx.ShortSessionID())

	lu := LoginUserFromContext(ctx)

	log.Debugf("auth (host): attempting key auth for %s: %s %s", lu, key.Type(), gossh.FingerprintSHA256(key))

	osUser, err := getOsUser(lu.User)
	if err != nil {
		log.Errorf("auth (host): %s", err)
		return false
	}

	if osUser.Uid != "0" && len(config.AllowedGroups) > 0 {
		userGroups, err := getUserGroups(osUser)
		if err != nil {
			log.Errorf("auth (host): %s", err)
			return false
		}

		if gid, match := groupMatch(config.AllowedGroups, userGroups); !match {
			log.Warnf("auth (host): no group match for %s %v in %v", lu.User, userGroups, config.AllowedGroups)
			return false
		} else {
			group, err := user.LookupGroupId(gid)
			if err != nil {
				log.Errorf("auth (host): %s", err)
			}
			log.Debugf("auth (host): host user %q matched %q group", lu.User, group.Name)
		}
	}

	keys, err := getUserAuthKeys(osUser)
	if err != nil {
		log.Errorf("auth (host): %s", err)
		return false
	}

	if len(keys) == 0 {
		log.Warnf("auth (host): no keys for %s", lu)
		return false
	}

	for _, k := range keys {
		equal, err := keysEqual(key, k)
		if err != nil {
			log.Errorf("auth (instance): failed to compare keys for %s: %s", lu, err)
		}
		if equal {
			log.Infof("auth (host): succeeded for %s: %s %s", lu, key.Type(), gossh.FingerprintSHA256(key))
			if !lu.IsValid() {
				return false
			}
			lu.PublicKey = key
			return true
		}
	}

	log.Warnf("auth (host): failed for %s: %s %s", lu, key.Type(), gossh.FingerprintSHA256(key))
	return false
}

// inAuthHandler performs host auth and instance auth
func inAuthHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	log := log.WithField("session", ctx.ShortSessionID())

	lu := LoginUserFromContext(ctx)

	// valid user on the host should be allowed
	valid := hostAuthHandler(ctx, key)
	if valid {
		return true
	} else {
		if !lu.IsValid() {
			return false
		}
	}

	// commands are allowed for host users only
	if lu.IsCommand() {
		return false
	}

	log.Debugf("auth (instance): attempting key auth for %s: %s %s", lu, key.Type(), gossh.FingerprintSHA256(key))

	client, err := NewDefaultIncusClientWithContext(ctx)
	if err != nil {
		log.Error(err)
		return false
	}

	// User handling
	iu, err := client.GetCachedInstanceUser(lu.Project, lu.Instance, lu.InstanceUser)
	if err != nil {
		log.Errorf("auth (instance): failed to get instance user %s for %s: %s", lu.InstanceUser, lu, err)
		return false
	}

	if iu == nil {
		log.Errorf("auth (instance): not found instance user for %s", lu)
		return false
	}

	path := iu.Dir + "/.ssh/authorized_keys"
	file, err := client.DownloadFile(iu.Project, iu.Instance, path)
	if err != nil {
		log.Warnf("auth (instance): failed to download %s for %s: %s", path, lu, err)
		return false
	}

	keys := file.Content.Lines()

	if len(keys) == 0 {
		log.Warnf("auth (instance): no keys for %s", lu)
		return false
	}

	for _, k := range keys {
		equal, err := keysEqual(key, k)
		if err != nil {
			log.Errorf("auth (instance): failed to compare keys for %s: %s", lu, err)
		}
		if equal {
			log.Infof("auth (instance): succeeded for %s: %s %s", lu, key.Type(), gossh.FingerprintSHA256(key))
			lu.PublicKey = key
			return true
		}
	}

	log.Warnf("auth (instance): failed for %s: %s %s", lu, key.Type(), gossh.FingerprintSHA256(key))
	return false
}

func noAuthHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	log := log.WithField("session", ctx.ShortSessionID())

	lu := LoginUserFromContext(ctx)
	log.Infof("auth (noauth): noauth login key for %s: %s %s", lu, key.Type(), gossh.FingerprintSHA256(key))
	if !lu.IsValid() {
		return false
	}
	lu.PublicKey = key
	return true
}

func keysEqual(key ssh.PublicKey, authKey []byte) (bool, error) {
	pkey, _, _, _, err := ssh.ParseAuthorizedKey(authKey)
	if err != nil {
		return false, err
	}
	return ssh.KeysEqual(pkey, key), nil
}
