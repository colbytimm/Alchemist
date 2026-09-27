package theme_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/colbytimm/alchemist/internal/theme"
)

func TestRoleNamesAreUniqueAndLowercase(t *testing.T) {
	lowercase := regexp.MustCompile(`^[a-z]+$`)
	seen := map[string]bool{}
	for _, role := range theme.Roles() {
		name := role.String()
		assert.Regexp(t, lowercase, name, "role %d", role)
		assert.False(t, seen[name], "%s is named twice", name)
		seen[name] = true
	}
}

func TestThereAreSeventeenRoles(t *testing.T) {
	assert.Len(t, theme.Roles(), 17)
}
