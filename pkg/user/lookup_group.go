//go:build !windows && go1.7
// +build !windows,go1.7

package user

import "strconv"

func lookupGroup(name string) (*Group, error) {
	if _, err := strconv.Atoi(name); err == nil {
		return nil, UnknownGroupError(name)
	}

	if dscacheutilExe != "" {
		g, err := dsGroup(name)
		if err == nil {
			return lgroup(g), nil
		}
	}

	g, err := getentGroup(name)
	if err == nil {
		return lgroup(g), nil
	}

	return nil, UnknownGroupError(name)
}

func lookupGroupId(gid string) (*Group, error) {
	_, err := strconv.Atoi(gid)
	if err != nil {
		return nil, err
	}

	if dscacheutilExe != "" {
		g, err := dsGroupId(gid)
		if err == nil {
			return lgroup(g), nil
		}
	}

	g, err := getentGroup(gid)
	if err == nil {
		return lgroup(g), nil
	}

	return nil, UnknownGroupIdError(gid)
}
