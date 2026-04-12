package project

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSlugPattern_Valid(t *testing.T) {
	valid := []string{
		"demo",
		"my-project",
		"project-01",
		"a-b",
		"abc",
	}
	for _, s := range valid {
		assert.True(t, slugPattern.MatchString(s), "expected valid: %q", s)
	}
}

func TestSlugPattern_Invalid(t *testing.T) {
	invalid := []string{
		"",
		"a",            // too short
		"-abc",         // starts with hyphen
		"abc-",         // ends with hyphen
		"ABC",          // uppercase
		"my project",   // space
		"my_project",   // underscore
		"my.project",   // dot
	}
	for _, s := range invalid {
		assert.False(t, slugPattern.MatchString(s), "expected invalid: %q", s)
	}
}
