//go:build go1.7
// +build go1.7

package user

import "os/user"

// Wrap user.Group with luser.Group
func lgroup(g *user.Group) *Group {
	return &Group{Group: g, IsLuser: true}
}
