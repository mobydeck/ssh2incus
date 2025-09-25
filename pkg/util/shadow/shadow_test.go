package shadow_test

import (
	"fmt"
	"testing"

	"ssh2incus/pkg/util/shadow"

	"github.com/stretchr/testify/assert"
)

func TestShadow(t *testing.T) {
	t.Run("null", func(t *testing.T) {
		null, err := shadow.LookupFile("root", "/dev/null")
		assert.Error(t, err, fmt.Sprintf("%s", err))
		assert.Nil(t, null)
	})

	t.Run("root", func(t *testing.T) {
		root, err := shadow.LookupFile("root", "test/shadow.txt")
		assert.Nil(t, err, fmt.Sprintf("%s", err))
		assert.True(t, root.IsAccountValid())
		assert.True(t, root.IsPasswordValid())
	})
	t.Run("nobody", func(t *testing.T) {
		root, err := shadow.LookupFile("nobody", "test/shadow.txt")
		assert.Nil(t, err, fmt.Sprintf("%s", err))
		assert.True(t, root.IsAccountValid())
		assert.True(t, root.IsPasswordValid())
	})

	t.Run("ubuntu", func(t *testing.T) {
		root, err := shadow.LookupFile("ubuntu", "test/shadow.txt")
		assert.Nil(t, err, fmt.Sprintf("%s", err))
		err = root.VerifyPassword("test")
		assert.Nil(t, err, fmt.Sprintf("%s", err))
		assert.True(t, root.IsPasswordValid())
	})

}
