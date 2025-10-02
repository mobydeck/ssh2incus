package server

import (
	"ssh2incus/pkg/ssh"
	"ssh2incus/pkg/util/shadow"

	log "github.com/sirupsen/logrus"
	gossh "golang.org/x/crypto/ssh"
)

func hostAuthHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	log := log.WithField("session", ctx.ShortSessionID())

	lu := LoginUserFromContext(ctx)

	log.Debugf("auth (host): attempting key auth for %s: %s %s", lu, key.Type(), gossh.FingerprintSHA256(key))

	hostUser, err := checkHostUser(lu)
	if err != nil {
		log.Errorf("auth (host): %v", err)
		return false
	}

	keys, err := getUserAuthKeys(hostUser)
	if err != nil {
		log.Errorf("auth (host): %s", err)
		return false
	}

	return authKeyCheck(ctx, key, lu, keys, "host")
}

// instanceAuthHandler performs host auth and instance auth
func instanceAuthHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	log := log.WithField("session", ctx.ShortSessionID())
	lu := LoginUserFromContext(ctx)

	// valid user on the host should be allowed
	if hostAuthHandler(ctx, key) {
		return true
	}
	if !lu.IsValid() {
		return false
	}

	// commands are allowed for host users only
	if lu.IsCommand() {
		return false
	}

	log.Debugf("auth (instance): attempting key auth for %s: %s %s", lu, key.Type(), gossh.FingerprintSHA256(key))

	client, err := NewDefaultIncusClientWithContext(ctx)
	if err != nil {
		log.Errorf("auth (instance): %s", err)
		return false
	}

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

	return authKeyCheck(ctx, key, lu, keys, "instance")
}

// authKeyCheck centralises key‑authentication logic.
func authKeyCheck(ctx ssh.Context, key ssh.PublicKey, lu *LoginUser, keys [][]byte, authDesc string) bool {
	log := log.WithField("session", ctx.ShortSessionID())

	//log.Debugf("auth (%s): attempting key auth for %s: %s %s", authDesc, lu, key.Type(), gossh.FingerprintSHA256(key))

	if len(keys) == 0 {
		log.Warnf("auth (%s): no keys for %s", authDesc, lu)
		return false
	}

	for _, k := range keys {
		equal, err := keysEqual(key, k)
		if err != nil {
			log.Errorf("auth (%s): failed to compare keys for %s: %s", authDesc, lu, err)
			continue
		}
		if equal {
			log.Infof("auth (%s): key check succeeded for %s: %s %s", authDesc, lu, key.Type(), gossh.FingerprintSHA256(key))
			if !lu.IsValid() {
				return false
			}
			lu.PublicKey = key
			return true
		}
	}

	log.Warnf("auth (%s): failed for %s: %s %s", authDesc, lu, key.Type(), gossh.FingerprintSHA256(key))
	return false
}

func passwordHandler(ctx ssh.Context, password string) bool {
	log := log.WithField("session", ctx.ShortSessionID())

	lu := LoginUserFromContext(ctx)

	log.Debugf("auth (host): attempting password auth for %s", lu)

	hostUser, err := checkHostUser(lu)
	if err != nil {
		log.Errorf("auth (host): %v", err)
		return false
	}

	err = checkHostShadowPassword(hostUser.Username, password)
	if err != nil {
		log.Errorf("auth (host): user %q: %v", hostUser.Username, err)
		if !config.InstanceAuth {
			return false
		}
	} else {
		log.Infof("auth (host): password check succeeded for %s", lu)
		return true
	}

	log.Debugf("auth (instance): attempting password auth for %s", lu)

	if !lu.IsValid() {
		return false
	}

	client, err := NewDefaultIncusClientWithContext(ctx)
	if err != nil {
		log.Errorf("failed to initialize incus client for %s: %v", lu, err)
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

	file, err := client.DownloadFile(iu.Project, iu.Instance, shadow.ShadowFile)
	if err != nil {
		log.Errorf("auth (instance): failed to download shadow file: %s", err)
		return false
	}

	shadowFile := string(file.Content.Bytes())
	err = checkShadowPassword(lu.InstanceUser, password, shadowFile)
	if err != nil {
		log.Errorf("auth (instance): failed to verify password: %s", err)
		return false
	}

	log.Infof("auth (instance): password check succeeded for %s", lu)
	return true
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
