package cleaner

import (
	"testing"
	"time"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
)

// ── Single-rule behaviour (backward-compat with old flat config) ──────────────

func TestMakeDecisions_Whitelisted_Group(t *testing.T) {
	s := defaultSettings()
	s.ProtectedGroups = []string{"protected"}

	decisions := map[string][]CleanupDecision{
		"protected": {
			decision("protected", "1.0.0", old),
			decision("protected", "2.0.0", old),
		},
	}

	stats := MakeDecisions(decisions, s)

	for _, d := range decisions["protected"] {
		assertAction(t, d, PROTECTED)
	}
	_ = stats
}

func TestMakeDecisions_Whitelisted_Version_InRule(t *testing.T) {
	s := singleRuleSettings(`^$`, 0, 0) // pattern matches nothing → unmatched
	s.Rules[0].Pattern = `.*`           // pattern matches everything
	s.Rules[0].WhitelistedVersions = []string{"pinned"}

	decisions := map[string][]CleanupDecision{
		"img": {
			decision("img", "pinned", old),
			decision("img", "1.0.0", old),
		},
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["img"][0], WHITELISTED)
	assertAction(t, decisions["img"][1], DELETE)
}

func TestMakeDecisions_Whitelisted_Artifact_InRule(t *testing.T) {
	s := singleRuleSettings(`.*`, 0, 0)
	s.Rules[0].WhitelistedArtifacts = []string{"img@pinned"}

	decisions := map[string][]CleanupDecision{
		"img": {
			decision("img", "pinned", old),
			decision("img", "1.0.0", old),
		},
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["img"][0], WHITELISTED)
	assertAction(t, decisions["img"][1], DELETE)
}

func TestMakeDecisions_RecentVersion_KeepsTopN(t *testing.T) {
	s := singleRuleSettings(`\d+\.\d+\.\d+`, 2, 0)

	decisions := map[string][]CleanupDecision{
		"img": {
			decision("img", "3.0.0", now.Add(-1*time.Hour)),
			decision("img", "2.0.0", now.Add(-2*time.Hour)),
			decision("img", "1.0.0", now.Add(-3*time.Hour)),
		},
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["img"][0], RECENT_VERSION)
	assertAction(t, decisions["img"][1], RECENT_VERSION)
	assertAction(t, decisions["img"][2], DELETE)
}

func TestMakeDecisions_RecentVersion_OnlyMatchingPattern(t *testing.T) {
	s := singleRuleSettings(`\d+\.\d+\.\d+`, 3, 0)

	// "latest" at i=0 doesn't match semver → unmatched → UNMATCHED_KEEP (unmatchedAction: keep)
	decisions := map[string][]CleanupDecision{
		"img": {
			decision("img", "latest", now.Add(-1*time.Hour)),
			decision("img", "1.0.0", now.Add(-2*time.Hour)),
		},
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["img"][0], UNMATCHED_KEEP) // no rule matched, default keep
	assertAction(t, decisions["img"][1], RECENT_VERSION)
}

func TestMakeDecisions_DownloadedRecently(t *testing.T) {
	s := singleRuleSettings(`.*`, 0, 90)

	decisions := map[string][]CleanupDecision{
		"img": {decisionDownloaded("img", "old-but-active", old, now.AddDate(0, 0, -10))},
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["img"][0], DOWNLOADED_RECENTLY)
}

func TestMakeDecisions_NilLastDownloaded_FallsBackToCreatedAt(t *testing.T) {
	s := singleRuleSettings(`.*`, 0, 90)

	decisions := map[string][]CleanupDecision{
		"img": {decision("img", "new", now.AddDate(0, 0, -5))},
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["img"][0], DOWNLOADED_RECENTLY)
}

func TestMakeDecisions_Delete_OldUnprotected(t *testing.T) {
	s := singleRuleSettings(`.*`, 0, 0)

	decisions := map[string][]CleanupDecision{
		"img": {decision("img", "stale", old)},
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["img"][0], DELETE)
}

func TestMakeDecisions_MultipleGroups_IndependentRetention(t *testing.T) {
	s := singleRuleSettings(`\d+\.\d+\.\d+`, 1, 0)

	decisions := map[string][]CleanupDecision{
		"img-a": {decision("img-a", "2.0.0", now), decision("img-a", "1.0.0", old)},
		"img-b": {decision("img-b", "2.0.0", now), decision("img-b", "1.0.0", old)},
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["img-a"][0], RECENT_VERSION)
	assertAction(t, decisions["img-a"][1], DELETE)
	assertAction(t, decisions["img-b"][0], RECENT_VERSION)
	assertAction(t, decisions["img-b"][1], DELETE)
}

// ── Multi-rule behaviour ──────────────────────────────────────────────────────

func TestMakeDecisions_MultiRule_IndependentRetentionCounts(t *testing.T) {
	s := TargetSettings{
		UnmatchedAction: "keep",
		Rules: []RuleSettings{
			{Name: "snapshots", Pattern: `.*-SNAPSHOT`, RecentArtifactRetention: 2, LastDownloadedDays: 0},
			{Name: "releases", Pattern: `\d+\.\d+\.\d+`, RecentArtifactRetention: 2, LastDownloadedDays: 0},
		},
	}

	// Interleaved snapshots and releases, sorted newest-first.
	decisions := map[string][]CleanupDecision{
		"img": {
			decision("img", "2.0.0", now.Add(-1*time.Hour)),         // release  count=1 → RECENT_VERSION
			decision("img", "2.0.0-SNAPSHOT", now.Add(-2*time.Hour)), // snapshot count=1 → RECENT_VERSION
			decision("img", "1.5.0", now.Add(-3*time.Hour)),         // release  count=2 → RECENT_VERSION
			decision("img", "1.5.0-SNAPSHOT", now.Add(-4*time.Hour)), // snapshot count=2 → RECENT_VERSION
			decision("img", "1.0.0", now.Add(-5*time.Hour)),         // release  count=3 > 2 → DELETE
			decision("img", "1.0.0-SNAPSHOT", now.Add(-6*time.Hour)), // snapshot count=3 > 2 → DELETE
		},
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["img"][0], RECENT_VERSION) // 2.0.0
	assertAction(t, decisions["img"][1], RECENT_VERSION) // 2.0.0-SNAPSHOT
	assertAction(t, decisions["img"][2], RECENT_VERSION) // 1.5.0
	assertAction(t, decisions["img"][3], RECENT_VERSION) // 1.5.0-SNAPSHOT
	assertAction(t, decisions["img"][4], DELETE)          // 1.0.0
	assertAction(t, decisions["img"][5], DELETE)          // 1.0.0-SNAPSHOT
}

func TestMakeDecisions_ProtectedVersions_BeforeRuleMatching(t *testing.T) {
	s := singleRuleSettings(`.*`, 0, 0)
	s.ProtectedVersions = []string{"latest"}

	decisions := map[string][]CleanupDecision{
		"img": {
			decision("img", "latest", old), // would be DELETE by rule, but protected first
			decision("img", "1.0.0", old),
		},
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["img"][0], PROTECTED)
	assertAction(t, decisions["img"][1], DELETE)
}

func TestMakeDecisions_ProtectedGroups_AllVersionsProtected(t *testing.T) {
	s := singleRuleSettings(`.*`, 0, 0)
	s.ProtectedGroups = []string{"base-image"}

	decisions := map[string][]CleanupDecision{
		"base-image": {
			decision("base-image", "1.0.0", old),
			decision("base-image", "2.0.0", old),
		},
		"regular-image": {
			decision("regular-image", "1.0.0", old),
		},
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["base-image"][0], PROTECTED)
	assertAction(t, decisions["base-image"][1], PROTECTED)
	assertAction(t, decisions["regular-image"][0], DELETE)
}

func TestMakeDecisions_UnmatchedAction_Keep(t *testing.T) {
	s := singleRuleSettings(`\d+\.\d+\.\d+`, 0, 0) // only semver matches
	s.UnmatchedAction = "keep"

	decisions := map[string][]CleanupDecision{
		"img": {decision("img", "latest", old)}, // doesn't match semver
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["img"][0], UNMATCHED_KEEP)
}

func TestMakeDecisions_UnmatchedAction_Delete(t *testing.T) {
	s := singleRuleSettings(`\d+\.\d+\.\d+`, 0, 0) // only semver matches
	s.UnmatchedAction = "delete"

	decisions := map[string][]CleanupDecision{
		"img": {decision("img", "latest", old)}, // doesn't match semver
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["img"][0], DELETE)
}

func TestMakeDecisions_WhitelistInRule_OnlyIfPatternMatched(t *testing.T) {
	// "pinned" is whitelisted inside the releases rule, but "pinned" doesn't
	// match \d+\.\d+\.\d+, so it will never be reached by that rule.
	// It falls through to unmatchedAction → UNMATCHED_KEEP.
	s := TargetSettings{
		UnmatchedAction: "keep",
		Rules: []RuleSettings{
			{
				Name:                "releases",
				Pattern:             `\d+\.\d+\.\d+`,
				RecentArtifactRetention: 5,
				WhitelistedVersions: []string{"pinned"},
			},
		},
	}

	decisions := map[string][]CleanupDecision{
		"img": {decision("img", "pinned", old)},
	}

	MakeDecisions(decisions, s)

	// "pinned" didn't match the rule pattern → rule whitelist unreachable → UNMATCHED_KEEP
	assertAction(t, decisions["img"][0], UNMATCHED_KEEP)
}

// ── artifactLifetimeDays grace period ────────────────────────────────────────

func TestMakeDecisions_ArtifactLifetimeDays_ProtectsNewUndownloadedArtifact(t *testing.T) {
	// lastDownloadedDays=1 would normally delete a 3-day-old never-downloaded artifact.
	// artifactLifetimeDays=7 saves it.
	s := TargetSettings{
		UnmatchedAction: "keep",
		Rules: []RuleSettings{{
			Name:                 "snapshots",
			Pattern:              `.*-SNAPSHOT`,
			RecentArtifactRetention: 0,
			LastDownloadedDays:   1,
			ArtifactLifetimeDays: 7,
		}},
	}

	decisions := map[string][]CleanupDecision{
		"img": {decision("img", "1.0.0-SNAPSHOT", now.AddDate(0, 0, -3))}, // 3 days old, never downloaded
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["img"][0], CREATED_RECENTLY)
}

func TestMakeDecisions_ArtifactLifetimeDays_DoesNotProtectOldArtifact(t *testing.T) {
	// 10-day-old artifact is outside the 7-day grace period → DELETE.
	s := TargetSettings{
		UnmatchedAction: "keep",
		Rules: []RuleSettings{{
			Name:                 "snapshots",
			Pattern:              `.*-SNAPSHOT`,
			RecentArtifactRetention: 0,
			LastDownloadedDays:   1,
			ArtifactLifetimeDays: 7,
		}},
	}

	decisions := map[string][]CleanupDecision{
		"img": {decision("img", "0.9.0-SNAPSHOT", now.AddDate(0, 0, -10))}, // 10 days old, never downloaded
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["img"][0], DELETE)
}

func TestMakeDecisions_ArtifactLifetimeDays_ZeroMeansDisabled(t *testing.T) {
	// artifactLifetimeDays=0 means no grace period — only lastDownloadedDays applies.
	s := singleRuleSettings(`.*`, 0, 1) // lastDownloadedDays=1

	decisions := map[string][]CleanupDecision{
		"img": {decision("img", "new", now.AddDate(0, 0, -3))},
	}

	MakeDecisions(decisions, s)

	assertAction(t, decisions["img"][0], DELETE)
}

// ── Old-cleaner-bug baseline ──────────────────────────────────────────────────

func TestMakeDecisions_Sha256TaggedPlatformImage_WouldBeDeleted(t *testing.T) {
	s := singleRuleSettings(`\d+\.\d+\.\d+`, 3, 0)

	decisions := map[string][]CleanupDecision{
		"my-image": {decision("my-image", "sha256:amd64deadbeef", old)},
	}

	MakeDecisions(decisions, s)

	// sha256:... doesn't match the semver pattern → unmatched, unmatchedAction: keep
	// so actually UNMATCHED_KEEP, not DELETE, with the new design!
	// The docker strategy adds them AFTER MakeDecisions via propagation.
	assertAction(t, decisions["my-image"][0], UNMATCHED_KEEP)
}

// ── BuildDecisionMap ──────────────────────────────────────────────────────────

func TestBuildDecisionMap_GroupsByGroup(t *testing.T) {
	artifacts := []artifactory.Metadata{
		{Path: "img-a/1.0.0", Group: "img-a", Version: "1.0.0", CreatedAt: ptr(now)},
		{Path: "img-b/1.0.0", Group: "img-b", Version: "1.0.0", CreatedAt: ptr(now)},
		{Path: "img-a/2.0.0", Group: "img-a", Version: "2.0.0", CreatedAt: ptr(old)},
	}

	dm := BuildDecisionMap(artifacts)

	if len(dm["img-a"]) != 2 {
		t.Errorf("img-a: got %d, want 2", len(dm["img-a"]))
	}
	if len(dm["img-b"]) != 1 {
		t.Errorf("img-b: got %d, want 1", len(dm["img-b"]))
	}
}

func TestBuildDecisionMap_SortedNewestFirst(t *testing.T) {
	t1 := now.Add(-1 * time.Hour)
	t2 := now.Add(-2 * time.Hour)
	t3 := now.Add(-3 * time.Hour)

	artifacts := []artifactory.Metadata{
		{Path: "img/1.0.0", Group: "img", Version: "1.0.0", CreatedAt: ptr(t3)},
		{Path: "img/3.0.0", Group: "img", Version: "3.0.0", CreatedAt: ptr(t1)},
		{Path: "img/2.0.0", Group: "img", Version: "2.0.0", CreatedAt: ptr(t2)},
	}

	dm := BuildDecisionMap(artifacts)
	g := dm["img"]

	if g[0].Artifact.Version != "3.0.0" || g[1].Artifact.Version != "2.0.0" || g[2].Artifact.Version != "1.0.0" {
		t.Errorf("wrong sort order: %v, %v, %v", g[0].Artifact.Version, g[1].Artifact.Version, g[2].Artifact.Version)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func assertAction(t *testing.T, d CleanupDecision, want CleanupAction) {
	t.Helper()
	if d.CleanupAction != want {
		t.Errorf("artifact %q: got %s, want %s",
			d.Artifact.Path,
			CleanupActionStrings[d.CleanupAction],
			CleanupActionStrings[want],
		)
	}
}

func withAction(d CleanupDecision, a CleanupAction) CleanupDecision {
	d.CleanupAction = a
	return d
}