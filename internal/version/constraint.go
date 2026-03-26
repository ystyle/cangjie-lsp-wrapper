package version

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

type Constraint struct {
	raw        string
	constraint *semver.Constraints
	org        string
	name       string
	versionReq string
}

func ParseConstraint(raw string) (*Constraint, error) {
	c := &Constraint{raw: raw, versionReq: raw}

	var err error
	c.constraint, err = semver.NewConstraint(convertToSemverConstraint(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid version constraint %q: %w", raw, err)
	}

	return c, nil
}

func (c *Constraint) MatchesVersion(version string) (bool, error) {
	if c.constraint == nil {
		return true, nil
	}

	v, err := semver.NewVersion(version)
	if err != nil {
		return false, err
	}

	return c.constraint.Check(v), nil
}

func (c *Constraint) String() string {
	return c.raw
}

func (c *Constraint) HasConstraint() bool {
	return c.constraint != nil && c.raw != ""
}

func (c *Constraint) Org() string {
	return c.org
}

func (c *Constraint) Name() string {
	return c.name
}

func convertToSemverConstraint(s string) string {
	s = strings.TrimSpace(s)

	if s == "" || s == "*" {
		return "*"
	}

	if !strings.HasPrefix(s, "[") && !strings.HasPrefix(s, "(") {
		return "= " + s
	}

	return parseRange(s)
}

func parseRange(s string) string {
	var isLowerInclusive, isUpperInclusive bool

	if strings.HasPrefix(s, "[") {
		isLowerInclusive = true
	}
	if strings.HasSuffix(s, "]") {
		isUpperInclusive = true
	}

	inner := s[1 : len(s)-1]
	parts := strings.SplitN(inner, ",", 2)

	var constraints []string

	if len(parts) >= 1 {
		lower := strings.TrimSpace(parts[0])
		if lower != "" {
			if isLowerInclusive {
				constraints = append(constraints, ">= "+lower)
			} else {
				constraints = append(constraints, "> "+lower)
			}
		}
	}

	if len(parts) >= 2 {
		upper := strings.TrimSpace(parts[1])
		if upper != "" {
			if isUpperInclusive {
				constraints = append(constraints, "<= "+upper)
			} else {
				constraints = append(constraints, "< "+upper)
			}
		}
	}

	if len(constraints) == 0 {
		return "*"
	}

	return strings.Join(constraints, " ")
}

var versionRangeRegex = regexp.MustCompile(`^[\[\(].*[\]\)]$`)

func IsVersionRange(s string) bool {
	return versionRangeRegex.MatchString(s)
}

func IsSingleVersion(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if versionRangeRegex.MatchString(s) {
		return false
	}
	_, err := semver.NewVersion(s)
	return err == nil
}
