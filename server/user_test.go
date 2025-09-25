package server

import (
	"testing"

	"github.com/muhlemmer/gu"
	"github.com/stretchr/testify/assert"
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
		"+remote:instance.project+alpine/edge+m2+c1+d0+e+n+p+v": {
			Remote:         "remote",
			User:           "root",
			Instance:       "instance",
			Project:        "project",
			InstanceUser:   "root",
			CreateInstance: true,
			CreateConfig: LoginCreateConfig{
				Image:      gu.Ptr("alpine/edge"),
				Memory:     gu.Ptr(2),
				Cpu:        gu.Ptr(1),
				Disk:       gu.Ptr(0),
				Ephemeral:  gu.Ptr(true),
				Nesting:    gu.Ptr(true),
				Privileged: gu.Ptr(true),
				Vm:         gu.Ptr(true),
			},
		},
		"~remote:instance.project+m2+vm": {
			Remote:         "remote",
			User:           "root",
			Instance:       "instance",
			Project:        "project",
			InstanceUser:   "root",
			CreateInstance: true,
			CreateConfig: LoginCreateConfig{
				Memory:    gu.Ptr(2),
				Ephemeral: gu.Ptr(true),
				Vm:        gu.Ptr(true),
			},
		},
		"~remote:instance.project+m2+vm+nest+priv": {
			Remote:         "remote",
			User:           "root",
			Instance:       "instance",
			Project:        "project",
			InstanceUser:   "root",
			CreateInstance: true,
			CreateConfig: LoginCreateConfig{
				Memory:     gu.Ptr(2),
				Ephemeral:  gu.Ptr(true),
				Nesting:    gu.Ptr(true),
				Privileged: gu.Ptr(true),
				Vm:         gu.Ptr(true),
			},
		},
		"~remote:instance.project+ubuntu/24.04+m4+c4+d20+v+n+p": {
			Remote:         "remote",
			User:           "root",
			Instance:       "instance",
			Project:        "project",
			InstanceUser:   "root",
			CreateInstance: true,
			CreateConfig: LoginCreateConfig{
				Image:      gu.Ptr("ubuntu/24.04"),
				Memory:     gu.Ptr(4),
				Cpu:        gu.Ptr(4),
				Disk:       gu.Ptr(20),
				Ephemeral:  gu.Ptr(true),
				Nesting:    gu.Ptr(true),
				Privileged: gu.Ptr(true),
				Vm:         gu.Ptr(true),
			},
		},
	}

	for us, lu := range cases {
		t.Run(us, func(t *testing.T) {
			u := parseLoginUser(us)
			assert.Equal(t, lu.Remote, u.Remote)
			assert.Equal(t, lu.Instance, u.Instance)
			assert.Equal(t, lu.Project, u.Project)
			assert.Equal(t, lu.InstanceUser, u.InstanceUser)
			assert.Equal(t, lu.User, u.User)
			assert.Equal(t, lu.CreateInstance, u.CreateInstance)
			assert.Equal(t, lu.CreateConfig, u.CreateConfig)
		})
	}
}
