package server

import (
	"testing"

	"github.com/muhlemmer/gu"
	"github.com/stretchr/testify/assert"
)

func TestLoginUser_ParseFrom(t *testing.T) {
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
		"%remote:iuser@instance.project~user": {
			Remote:       "remote",
			User:         "user",
			Instance:     "instance",
			Project:      "project",
			InstanceUser: "iuser",
			Persistent:   true,
		},
		"/shell": {
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
				CPU:        gu.Ptr(1),
				Disk:       gu.Ptr(0),
				Ephemeral:  gu.Ptr(true),
				Nesting:    gu.Ptr(true),
				Privileged: gu.Ptr(true),
				VM:         gu.Ptr(true),
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
				VM:        gu.Ptr(true),
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
				VM:         gu.Ptr(true),
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
				CPU:        gu.Ptr(4),
				Disk:       gu.Ptr(20),
				Ephemeral:  gu.Ptr(true),
				Nesting:    gu.Ptr(true),
				Privileged: gu.Ptr(true),
				VM:         gu.Ptr(true),
			},
		},
		"~remote:test@instance.project+%web+%db+ubuntu/24.04+m4+c4+d20+v+n+p~admin": {
			Remote:         "remote",
			User:           "admin",
			Instance:       "instance",
			Project:        "project",
			InstanceUser:   "test",
			CreateInstance: true,
			CreateConfig: LoginCreateConfig{
				Image:      gu.Ptr("ubuntu/24.04"),
				Memory:     gu.Ptr(4),
				CPU:        gu.Ptr(4),
				Disk:       gu.Ptr(20),
				Ephemeral:  gu.Ptr(true),
				Nesting:    gu.Ptr(true),
				Privileged: gu.Ptr(true),
				VM:         gu.Ptr(true),
				Profiles:   []string{"web", "db"},
			},
		},
		"~inc1:test1@db-web.cool+ubuntu/24.04/cloud+m1+c2+d40+v+n+p+%db+%web~admin": {
			Remote:         "inc1",
			User:           "admin",
			Instance:       "db-web",
			Project:        "cool",
			InstanceUser:   "test1",
			CreateInstance: true,
			CreateConfig: LoginCreateConfig{
				Image:      gu.Ptr("ubuntu/24.04/cloud"),
				Memory:     gu.Ptr(1),
				CPU:        gu.Ptr(2),
				Disk:       gu.Ptr(40),
				Ephemeral:  gu.Ptr(true),
				Nesting:    gu.Ptr(true),
				Privileged: gu.Ptr(true),
				VM:         gu.Ptr(true),
				Profiles:   []string{"db", "web"},
			},
		},
	}

	for us, expected := range cases {
		t.Run(us, func(t *testing.T) {
			u := &LoginUser{}
			u.ParseFrom(us)
			assert.Equal(t, expected.Remote, u.Remote)
			assert.Equal(t, expected.Instance, u.Instance)
			assert.Equal(t, expected.Project, u.Project)
			assert.Equal(t, expected.InstanceUser, u.InstanceUser)
			assert.Equal(t, expected.User, u.User)
			assert.Equal(t, expected.CreateInstance, u.CreateInstance)
			assert.Equal(t, expected.CreateConfig, u.CreateConfig)
			assert.Equal(t, expected.Command, u.Command)
			assert.Equal(t, expected.Persistent, u.Persistent)
		})
	}
}

func TestLoginUser_ParseFrom_PersistentSession(t *testing.T) {
	cases := map[string]LoginUser{
		"%instance": {
			User:         "root",
			Instance:     "instance",
			Project:      "default",
			InstanceUser: "root",
			Persistent:   true,
		},
		"%remote:instance.project+user": {
			Remote:       "remote",
			User:         "user",
			Instance:     "instance",
			Project:      "project",
			InstanceUser: "root",
			Persistent:   true,
		},
	}

	for us, expected := range cases {
		t.Run(us, func(t *testing.T) {
			u := &LoginUser{}
			u.ParseFrom(us)
			assert.Equal(t, expected.Persistent, u.Persistent)
			assert.Equal(t, expected.Remote, u.Remote)
			assert.Equal(t, expected.Instance, u.Instance)
			assert.Equal(t, expected.Project, u.Project)
			assert.Equal(t, expected.User, u.User)
		})
	}
}

func TestLoginUser_ParseFrom_EdgeCases(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		u := &LoginUser{}
		u.ParseFrom("")
		assert.Equal(t, "root", u.User)
		assert.Equal(t, "root", u.InstanceUser)
		assert.Equal(t, "default", u.Project)
		assert.Equal(t, "", u.Instance)
	})

	t.Run("only project", func(t *testing.T) {
		u := &LoginUser{}
		u.ParseFrom(".myproject")
		assert.Equal(t, "", u.Instance)
		assert.Equal(t, "myproject", u.Project)
	})
}

func TestParseLoginUser_BackwardCompatibility(t *testing.T) {
	testCases := []string{
		"instance",
		"instance+user",
		"remote:instance.project+user",
		"+remote:instance.project+alpine/edge+m2+c1",
		"~instance+ubuntu/22.04+vm",
		"/shell",
		"%instance.project",
	}

	for _, userStr := range testCases {
		t.Run(userStr, func(t *testing.T) {
			// Test old function
			oldResult := parseLoginUser(userStr)

			// Test new method
			newResult := &LoginUser{}
			newResult.ParseFrom(userStr)

			// They should produce identical results
			assert.Equal(t, oldResult.Remote, newResult.Remote)
			assert.Equal(t, oldResult.Instance, newResult.Instance)
			assert.Equal(t, oldResult.Project, newResult.Project)
			assert.Equal(t, oldResult.InstanceUser, newResult.InstanceUser)
			assert.Equal(t, oldResult.User, newResult.User)
			assert.Equal(t, oldResult.CreateInstance, newResult.CreateInstance)
			assert.Equal(t, oldResult.CreateConfig, newResult.CreateConfig)
			assert.Equal(t, oldResult.Command, newResult.Command)
			assert.Equal(t, oldResult.Persistent, newResult.Persistent)
			assert.Equal(t, oldResult.OrigUser, newResult.OrigUser)
		})
	}
}
