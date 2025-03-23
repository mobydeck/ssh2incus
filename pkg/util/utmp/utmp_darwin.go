//go:build darwin
// +build darwin

package utmp

type UtmpEntry struct {
	entry struct{}
}

func Put(user, ptsName, host string) UtmpEntry {
	var entry UtmpEntry
	return entry
}

// Remove a username/host entry from utmp
func Unput(entry UtmpEntry) {
}

// Put the login app, username and originating host/IP to lastlog
func PutLastlogEntry(app, usr, ptsname, host string) {
}
