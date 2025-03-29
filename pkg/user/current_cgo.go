//go:build !windows && cgo
// +build !windows,cgo

package user

import "os/user"

func currentUser() (*User, error) {
	u, err := user.Current()
	if err != nil {
		return nil, err
	}
	return &User{User: u}, nil
}
