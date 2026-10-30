package deckwizard

import (
	"regexp"
	"testing"

	"manafold-backend/internal/deckroles"
)

// compilePrefilter compiles a role's SQL prefilter as a case-insensitive Go
// regexp. The patterns stay inside the syntax both engines share.
func compilePrefilter(t *testing.T, role deckroles.Role) *regexp.Regexp {
	t.Helper()
	p := RolePrefilter(role)
	if p == "" {
		t.Fatalf("no prefilter for %s", role)
	}
	re, err := regexp.Compile("(?i)" + p)
	if err != nil {
		t.Fatalf("prefilter for %s does not compile: %v", role, err)
	}
	return re
}
