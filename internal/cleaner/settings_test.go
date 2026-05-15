package cleaner

import (
	"os"
	"strings"
	"testing"
)

// ── LoadCleanupProperties ─────────────────────────────────────────────────────

func TestLoadCleanupProperties_ParsesCorrectly(t *testing.T) {
	yaml := `
cleanup:
  repositories:
    docker:
      name: docker-local
      unmatchedAction: keep
      protectedVersions: [latest]
      protectedGroups: [busybox]
      rules:
        - name: snapshots
          pattern: ".*-SNAPSHOT.*"
          recentArtifactRetention: 3
          lastDownloadedDays: 7
        - name: releases
          pattern: "\\d+\\.\\d+\\.\\d+"
          recentArtifactRetention: 5
          lastDownloadedDays: 90
`
	f := writeTempFile(t, yaml)
	props, err := LoadCleanupProperties(f)
	if err != nil {
		t.Fatal(err)
	}

	repo, ok := props.Cleanup.Repositories["docker"]
	if !ok {
		t.Fatal("repository 'docker' not found")
	}
	if repo.Name != "docker-local" {
		t.Errorf("Name = %q, want docker-local", repo.Name)
	}
	if repo.UnmatchedAction != "keep" {
		t.Errorf("UnmatchedAction = %q, want keep", repo.UnmatchedAction)
	}
	if len(repo.ProtectedVersions) != 1 || repo.ProtectedVersions[0] != "latest" {
		t.Errorf("ProtectedVersions = %v, want [latest]", repo.ProtectedVersions)
	}
	if len(repo.Rules) != 2 {
		t.Errorf("Rules len = %d, want 2", len(repo.Rules))
	}
	if repo.Rules[0].Name != "snapshots" || repo.Rules[1].Name != "releases" {
		t.Errorf("rule names = %q, %q", repo.Rules[0].Name, repo.Rules[1].Name)
	}
}

func TestLoadCleanupProperties_MissingFile(t *testing.T) {
	_, err := LoadCleanupProperties("/nonexistent/path/cleanup.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// ── ValidateSettings ──────────────────────────────────────────────────────────

func TestValidateSettings_WarnOnUnreachableWhitelistedVersion(t *testing.T) {
	s := TargetSettings{
		Rules: []RuleSettings{
			{
				Name:                "releases",
				Pattern:             `\d+\.\d+\.\d+`,
				WhitelistedVersions: []string{"latest"}, // doesn't match semver
			},
		},
	}

	warnings := ValidateSettings("docker", s)

	if !anyContains(warnings, "latest") {
		t.Errorf("expected warning about 'latest' being unreachable, got: %v", warnings)
	}
}

func TestValidateSettings_WarnOnUnreachableWhitelistedArtifact(t *testing.T) {
	s := TargetSettings{
		Rules: []RuleSettings{
			{
				Name:                 "releases",
				Pattern:              `\d+\.\d+\.\d+`,
				WhitelistedArtifacts: []string{"img@latest-SNAPSHOT"}, // version doesn't match semver
			},
		},
	}

	warnings := ValidateSettings("docker", s)

	if !anyContains(warnings, "latest-SNAPSHOT") {
		t.Errorf("expected warning about version in whitelistedArtifact, got: %v", warnings)
	}
}

func TestValidateSettings_WarnOnProtectedVersionMatchingRulePattern(t *testing.T) {
	// "1.5.2" matches the releases pattern — protection takes priority but
	// any whitelist entry for it inside the rule is redundant.
	s := TargetSettings{
		ProtectedVersions: []string{"1.5.2"},
		Rules: []RuleSettings{
			{Name: "releases", Pattern: `\d+\.\d+\.\d+`},
		},
	}

	warnings := ValidateSettings("docker", s)

	if !anyContains(warnings, "1.5.2") {
		t.Errorf("expected warning about protectedVersion matching rule pattern, got: %v", warnings)
	}
}

func TestValidateSettings_NoWarningsForCorrectConfig(t *testing.T) {
	s := TargetSettings{
		ProtectedVersions: []string{"latest"}, // doesn't match semver → no warning
		Rules: []RuleSettings{
			{
				Name:                "releases",
				Pattern:             `\d+\.\d+\.\d+`,
				WhitelistedVersions: []string{"1.5.2"}, // matches semver → no warning
			},
		},
	}

	warnings := ValidateSettings("docker", s)

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got: %v", warnings)
	}
}

func TestValidateSettings_WarnOnInvalidPattern(t *testing.T) {
	s := TargetSettings{
		Rules: []RuleSettings{
			{Name: "bad", Pattern: `[invalid`},
		},
	}

	warnings := ValidateSettings("docker", s)

	if len(warnings) == 0 {
		t.Error("expected warning for invalid regexp, got none")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func anyContains(warnings []Warning, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w.Message, substr) {
			return true
		}
	}
	return false
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "cleanup_*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}