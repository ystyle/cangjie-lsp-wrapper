package version

import (
	"testing"
)

func TestParseConstraint_SingleVersion(t *testing.T) {
	c, err := ParseConstraint("1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !c.HasConstraint() {
		t.Error("expected constraint to exist")
	}

	matched, err := c.MatchesVersion("1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Error("expected 1.0.0 to match")
	}

	matched, _ = c.MatchesVersion("1.0.1")
	if matched {
		t.Error("expected 1.0.1 not to match")
	}
}

func TestParseConstraint_VersionRange(t *testing.T) {
	tests := []struct {
		constraint string
		version    string
		expected   bool
	}{
		{"[1.0.0, 3.0.0)", "1.0.0", true},
		{"[1.0.0, 3.0.0)", "2.0.0", true},
		{"[1.0.0, 3.0.0)", "2.9.9", true},
		{"[1.0.0, 3.0.0)", "3.0.0", false},
		{"[1.0.0, 3.0.0)", "0.9.0", false},
		{"(1.0.0, 3.0.0]", "1.0.0", false},
		{"(1.0.0, 3.0.0]", "3.0.0", true},
		{"[1.0.0, 3.0.0]", "1.0.0", true},
		{"[1.0.0, 3.0.0]", "3.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.constraint+"_"+tt.version, func(t *testing.T) {
			c, err := ParseConstraint(tt.constraint)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			matched, err := c.MatchesVersion(tt.version)
			if err != nil {
				t.Fatalf("unexpected error matching version: %v", err)
			}

			if matched != tt.expected {
				t.Errorf("constraint %s matching version %s: got %v, want %v", tt.constraint, tt.version, matched, tt.expected)
			}
		})
	}
}

func TestIsVersionRange(t *testing.T) {
	if !IsVersionRange("[1.0.0, 2.0.0)") {
		t.Error("expected [1.0.0, 2.0.0) to be a version range")
	}
	if !IsVersionRange("(1.0.0, 2.0.0]") {
		t.Error("expected (1.0.0, 2.0.0] to be a version range")
	}
	if IsVersionRange("1.0.0") {
		t.Error("expected 1.0.0 not to be a version range")
	}
}

func TestIsSingleVersion(t *testing.T) {
	if !IsSingleVersion("1.0.0") {
		t.Error("expected 1.0.0 to be a single version")
	}
	if !IsSingleVersion("2.3.4") {
		t.Error("expected 2.3.4 to be a single version")
	}
	if IsSingleVersion("[1.0.0, 2.0.0)") {
		t.Error("expected [1.0.0, 2.0.0) not to be a single version")
	}
	if IsSingleVersion("") {
		t.Error("expected empty string not to be a single version")
	}
}
