package server

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestParseUser(t *testing.T) {
	cases := map[string]LoginUser{
		"instance": {
			User:         "root",
			Instance:     "instance",
			Project:      "default",
			InstanceUser: "root",
		},
		"instance+user": {
			User:         "user",
			Instance:     "instance",
			Project:      "default",
			InstanceUser: "root",
		},
		"instance.project+user": {
			User:         "user",
			Instance:     "instance",
			Project:      "project",
			InstanceUser: "root",
		},
		"iuser@instance.project+user": {
			User:         "user",
			Instance:     "instance",
			Project:      "project",
			InstanceUser: "iuser",
		},
		"iuser@instance.project": {
			User:         "root",
			Instance:     "instance",
			Project:      "project",
			InstanceUser: "iuser",
		},
		"iuser@instance": {
			User:         "root",
			Instance:     "instance",
			Project:      "default",
			InstanceUser: "iuser",
		},
		"remote:iuser@instance": {
			Remote:       "remote",
			User:         "root",
			Instance:     "instance",
			Project:      "default",
			InstanceUser: "iuser",
		},
		"remote:iuser@instance.project+user": {
			Remote:       "remote",
			User:         "user",
			Instance:     "instance",
			Project:      "project",
			InstanceUser: "iuser",
		},
		"%shell": {
			User:    "root",
			Command: "shell",
		},
	}

	for us, lu := range cases {
		t.Run(us, func(t *testing.T) {
			u := parseLoginUser(us)
			assert.Equal(t, lu.Instance, u.Instance)
			assert.Equal(t, u.InstanceUser, u.InstanceUser)
			assert.Equal(t, u.Project, u.Project)
			assert.Equal(t, u.User, u.User)
		})
	}
}
