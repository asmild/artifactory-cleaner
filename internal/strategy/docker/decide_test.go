package docker

import (
	"testing"
	"time"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
	"github.com/asmild/artifactory-cleaner/internal/cleaner"
)

var (
	now         = time.Now()
	releaseRule = &cleaner.RuleSettings{
		Name:                    "releases",
		RecentArtifactRetention: 3,
		LastDownloadedDays:      90,
		ArtifactLifetimeDays:    7,
	}
)

// ── helpers ──────────────────────────────────────────────────────────────────

func pendingDecision(group, version string, createdAt time.Time, rule *cleaner.RuleSettings) *cleaner.CleanupDecision {
	return &cleaner.CleanupDecision{
		CleanupAction: pending,
		RuleSettings:  rule,
		Artifact: artifactory.Metadata{
			Group:     group,
			Version:   version,
			CreatedAt: &createdAt,
		},
	}
}

func pendingPlatform(group, version string, parents ...string) *cleaner.CleanupDecision {
	return &cleaner.CleanupDecision{
		CleanupAction: pending,
		Artifact: artifactory.Metadata{
			Group:   group,
			Version: version,
			Parent:  parents,
		},
	}
}

func settings(protectedGroups, protectedVersions []string, unmatchedAction string) cleaner.TargetSettings {
	return cleaner.TargetSettings{
		ProtectedGroups:   protectedGroups,
		ProtectedVersions: protectedVersions,
		UnmatchedAction:   unmatchedAction,
	}
}

// ── resolveRetentionCounts ───────────────────────────────────────────────────

func TestResolveRetentionCounts_KeepsNewest(t *testing.T) {
	decisions := map[string]*cleaner.CleanupDecision{
		"img/1.0": pendingDecision("img", "1.0", now.AddDate(0, 0, -10), releaseRule),
		"img/2.0": pendingDecision("img", "2.0", now.AddDate(0, 0, -5), releaseRule),
		"img/3.0": pendingDecision("img", "3.0", now.AddDate(0, 0, -1), releaseRule),
		"img/4.0": pendingDecision("img", "4.0", now, releaseRule),
	}

	resolveRetentionCounts(decisions)

	assertAction(t, decisions, "img/1.0", cleaner.DELETE)
	assertAction(t, decisions, "img/2.0", cleaner.RECENT_VERSION)
	assertAction(t, decisions, "img/3.0", cleaner.RECENT_VERSION)
	assertAction(t, decisions, "img/4.0", cleaner.RECENT_VERSION)
}

func TestResolveRetentionCounts_PlatformsDoNotConsumeSlots(t *testing.T) {
	// 3 manifest list versions + platforms — platforms must not consume retention slots
	decisions := map[string]*cleaner.CleanupDecision{
		"img/1.0":              pendingDecision("img", "1.0", now.AddDate(0, 0, -10), releaseRule),
		"img/2.0":              pendingDecision("img", "2.0", now.AddDate(0, 0, -5), releaseRule),
		"img/3.0":              pendingDecision("img", "3.0", now, releaseRule),
		"img/sha256:aaa/1.0":  pendingPlatform("img/sha256:aaa", "1.0", "img/1.0"),
		"img/sha256:bbb/2.0":  pendingPlatform("img/sha256:bbb", "2.0", "img/2.0"),
		"img/sha256:ccc/3.0":  pendingPlatform("img/sha256:ccc", "3.0", "img/3.0"),
	}

	resolveRetentionCounts(decisions)

	// all 3 root versions fit in retention=3
	assertAction(t, decisions, "img/1.0", cleaner.RECENT_VERSION)
	assertAction(t, decisions, "img/2.0", cleaner.RECENT_VERSION)
	assertAction(t, decisions, "img/3.0", cleaner.RECENT_VERSION)
	// platforms remain pending — propagation handles them
	assertAction(t, decisions, "img/sha256:aaa/1.0", pending)
	assertAction(t, decisions, "img/sha256:bbb/2.0", pending)
	assertAction(t, decisions, "img/sha256:ccc/3.0", pending)
}

func TestResolveRetentionCounts_CountsAcrossManifestListsAndStandalones(t *testing.T) {
	// 2 manifest list versions + 2 standalone versions for the same image — retention=3
	// oldest should be deleted regardless of type
	decisions := map[string]*cleaner.CleanupDecision{
		"img/1.0":        pendingDecision("img", "1.0", now.AddDate(0, 0, -20), releaseRule),
		"img/2.0":        pendingDecision("img", "2.0", now.AddDate(0, 0, -10), releaseRule),
		"img/standalone": pendingDecision("img", "3.0", now.AddDate(0, 0, -5), releaseRule),
		"img/4.0":        pendingDecision("img", "4.0", now, releaseRule),
	}

	resolveRetentionCounts(decisions)

	assertAction(t, decisions, "img/1.0", cleaner.DELETE)
	assertAction(t, decisions, "img/2.0", cleaner.RECENT_VERSION)
	assertAction(t, decisions, "img/standalone", cleaner.RECENT_VERSION)
	assertAction(t, decisions, "img/4.0", cleaner.RECENT_VERSION)
}

func TestResolveRetentionCounts_SkipsNilRule(t *testing.T) {
	decisions := map[string]*cleaner.CleanupDecision{
		"img/1.0": pendingDecision("img", "1.0", now, nil),
	}

	resolveRetentionCounts(decisions)

	assertAction(t, decisions, "img/1.0", pending)
}

func TestResolveRetentionCounts_IndependentCountersPerRule(t *testing.T) {
	ruleA := &cleaner.RuleSettings{Name: "snapshots", RecentArtifactRetention: 2}
	ruleB := &cleaner.RuleSettings{Name: "releases", RecentArtifactRetention: 1}

	decisions := map[string]*cleaner.CleanupDecision{
		"img/snap-1": pendingDecision("img", "snap-1", now.AddDate(0, 0, -5), ruleA),
		"img/snap-2": pendingDecision("img", "snap-2", now.AddDate(0, 0, -1), ruleA),
		"img/snap-3": pendingDecision("img", "snap-3", now, ruleA),
		"img/1.0":    pendingDecision("img", "1.0", now.AddDate(0, 0, -3), ruleB),
		"img/2.0":    pendingDecision("img", "2.0", now, ruleB),
	}

	resolveRetentionCounts(decisions)

	assertAction(t, decisions, "img/snap-1", cleaner.DELETE)
	assertAction(t, decisions, "img/snap-2", cleaner.RECENT_VERSION)
	assertAction(t, decisions, "img/snap-3", cleaner.RECENT_VERSION)
	assertAction(t, decisions, "img/1.0", cleaner.DELETE)
	assertAction(t, decisions, "img/2.0", cleaner.RECENT_VERSION)
}

func TestResolveRetentionCounts_IndependentCountersPerImageGroup(t *testing.T) {
	// two different images — each gets its own retention slots
	decisions := map[string]*cleaner.CleanupDecision{
		"img-a/1.0": pendingDecision("img-a", "1.0", now.AddDate(0, 0, -5), releaseRule),
		"img-a/2.0": pendingDecision("img-a", "2.0", now, releaseRule),
		"img-b/1.0": pendingDecision("img-b", "1.0", now.AddDate(0, 0, -5), releaseRule),
		"img-b/2.0": pendingDecision("img-b", "2.0", now, releaseRule),
	}

	resolveRetentionCounts(decisions)

	assertAction(t, decisions, "img-a/1.0", cleaner.RECENT_VERSION)
	assertAction(t, decisions, "img-a/2.0", cleaner.RECENT_VERSION)
	assertAction(t, decisions, "img-b/1.0", cleaner.RECENT_VERSION)
	assertAction(t, decisions, "img-b/2.0", cleaner.RECENT_VERSION)
}

// ── makeDecision ─────────────────────────────────────────────────────────────

func TestMakeDecision_Protected_Version(t *testing.T) {
	s := settings(nil, []string{"latest"}, "keep")
	artifact := &artifactory.Metadata{Group: "img", Version: "latest"}
	if got := makeDecision(artifact, releaseRule, &s); got != cleaner.PROTECTED {
		t.Errorf("got %v, want PROTECTED", got)
	}
}

func TestMakeDecision_Protected_Group(t *testing.T) {
	s := settings([]string{"img"}, nil, "keep")
	artifact := &artifactory.Metadata{Group: "img", Version: "1.0"}
	if got := makeDecision(artifact, releaseRule, &s); got != cleaner.PROTECTED {
		t.Errorf("got %v, want PROTECTED", got)
	}
}

func TestMakeDecision_Protected_SubGroup(t *testing.T) {
	s := settings([]string{"infobip"}, nil, "keep")
	artifact := &artifactory.Metadata{Group: "infobip/myservice", Version: "1.0"}
	if got := makeDecision(artifact, releaseRule, &s); got != cleaner.PROTECTED {
		t.Errorf("got %v, want PROTECTED", got)
	}
}

func TestMakeDecision_UnmatchedKeep(t *testing.T) {
	s := settings(nil, nil, "keep")
	artifact := &artifactory.Metadata{Group: "img", Version: "1.0"}
	if got := makeDecision(artifact, nil, &s); got != cleaner.UNMATCHED_KEEP {
		t.Errorf("got %v, want UNMATCHED_KEEP", got)
	}
}

func TestMakeDecision_UnmatchedDelete(t *testing.T) {
	s := settings(nil, nil, "delete")
	artifact := &artifactory.Metadata{Group: "img", Version: "1.0"}
	if got := makeDecision(artifact, nil, &s); got != cleaner.DELETE {
		t.Errorf("got %v, want DELETE", got)
	}
}

func TestMakeDecision_Whitelisted_Version(t *testing.T) {
	s := settings(nil, nil, "keep")
	rule := &cleaner.RuleSettings{Name: "r", WhitelistedVersions: []string{"1.0"}}
	artifact := &artifactory.Metadata{Group: "img", Version: "1.0"}
	if got := makeDecision(artifact, rule, &s); got != cleaner.WHITELISTED {
		t.Errorf("got %v, want WHITELISTED", got)
	}
}

func TestMakeDecision_Whitelisted_Group(t *testing.T) {
	s := settings(nil, nil, "keep")
	rule := &cleaner.RuleSettings{Name: "r", WhitelistedGroups: []string{"img"}}
	artifact := &artifactory.Metadata{Group: "img", Version: "1.0"}
	if got := makeDecision(artifact, rule, &s); got != cleaner.WHITELISTED {
		t.Errorf("got %v, want WHITELISTED", got)
	}
}

func TestMakeDecision_Whitelisted_Artifact(t *testing.T) {
	s := settings(nil, nil, "keep")
	rule := &cleaner.RuleSettings{Name: "r", WhitelistedArtifacts: []string{"img@1.0"}}
	artifact := &artifactory.Metadata{Group: "img", Version: "1.0"}
	if got := makeDecision(artifact, rule, &s); got != cleaner.WHITELISTED {
		t.Errorf("got %v, want WHITELISTED", got)
	}
}

func TestMakeDecision_CreatedRecently(t *testing.T) {
	s := settings(nil, nil, "keep")
	rule := &cleaner.RuleSettings{Name: "r", ArtifactLifetimeDays: 7}
	createdAt := now.AddDate(0, 0, -3)
	artifact := &artifactory.Metadata{Group: "img", Version: "1.0", CreatedAt: &createdAt}
	if got := makeDecision(artifact, rule, &s); got != cleaner.CREATED_RECENTLY {
		t.Errorf("got %v, want CREATED_RECENTLY", got)
	}
}

func TestMakeDecision_OlderThanLifetime_ReturnsPending(t *testing.T) {
	s := settings(nil, nil, "keep")
	rule := &cleaner.RuleSettings{Name: "r", ArtifactLifetimeDays: 7, RecentArtifactRetention: 3}
	createdAt := now.AddDate(0, 0, -10)
	artifact := &artifactory.Metadata{Group: "img", Version: "1.0", CreatedAt: &createdAt}
	if got := makeDecision(artifact, rule, &s); got != pending {
		t.Errorf("got %v, want pending", got)
	}
}

func TestMakeDecision_ProtectedBeforeRule(t *testing.T) {
	// protected must win even if a rule matches and whitelists would apply
	s := settings([]string{"img"}, nil, "keep")
	rule := &cleaner.RuleSettings{Name: "r", WhitelistedGroups: []string{"img"}}
	artifact := &artifactory.Metadata{Group: "img", Version: "1.0"}
	if got := makeDecision(artifact, rule, &s); got != cleaner.PROTECTED {
		t.Errorf("got %v, want PROTECTED (not WHITELISTED)", got)
	}
}

// ── propagatePlatformDecisions ───────────────────────────────────────────────

func TestPropagate_SingleParentDeleted_PlatformDeleted(t *testing.T) {
	decisions := map[string]*cleaner.CleanupDecision{
		"img/1.0-SNAPSHOT":     {CleanupAction: cleaner.DELETE, Artifact: artifactory.Metadata{Path: "img/1.0-SNAPSHOT", Group: "img", Version: "1.0-SNAPSHOT"}},
		"img/sha256:aaa":       pendingPlatform("img", "sha256:aaa", "img/1.0-SNAPSHOT"),
	}

	propagatePlatformDecisions(decisions)

	assertAction(t, decisions, "img/sha256:aaa", cleaner.DELETE)
}

func TestPropagate_SingleParentKept_PlatformManifestListRef(t *testing.T) {
	decisions := map[string]*cleaner.CleanupDecision{
		"img/1.0":        {CleanupAction: cleaner.RECENT_VERSION, Artifact: artifactory.Metadata{Path: "img/1.0", Group: "img", Version: "1.0"}},
		"img/sha256:aaa": pendingPlatform("img", "sha256:aaa", "img/1.0"),
	}

	propagatePlatformDecisions(decisions)

	assertAction(t, decisions, "img/sha256:aaa", cleaner.MANIFEST_LIST_REF)
}

func TestPropagate_SharedDigest_OneParentDeletedOneKept_PlatformKept(t *testing.T) {
	// sha256:aaa is referenced by both:
	//   - 1.0-SNAPSHOT → DELETE (snapshot retention exhausted)
	//   - master       → KEEP_UNMATCHED (no rule matched)
	// Platform must survive because master still uses it.
	decisions := map[string]*cleaner.CleanupDecision{
		"img/1.0-SNAPSHOT": {CleanupAction: cleaner.DELETE, Artifact: artifactory.Metadata{Path: "img/1.0-SNAPSHOT"}},
		"img/master":       {CleanupAction: cleaner.UNMATCHED_KEEP, Artifact: artifactory.Metadata{Path: "img/master"}},
		"img/sha256:aaa":   pendingPlatform("img", "sha256:aaa", "img/1.0-SNAPSHOT", "img/master"),
	}

	propagatePlatformDecisions(decisions)

	assertAction(t, decisions, "img/sha256:aaa", cleaner.MANIFEST_LIST_REF)
}

func TestPropagate_SharedDigest_AllParentsDeleted_PlatformDeleted(t *testing.T) {
	// Both snapshot versions deleted — platform should be deleted too.
	decisions := map[string]*cleaner.CleanupDecision{
		"img/1.0-SNAPSHOT": {CleanupAction: cleaner.DELETE, Artifact: artifactory.Metadata{Path: "img/1.0-SNAPSHOT"}},
		"img/2.0-SNAPSHOT": {CleanupAction: cleaner.DELETE, Artifact: artifactory.Metadata{Path: "img/2.0-SNAPSHOT"}},
		"img/sha256:aaa":   pendingPlatform("img", "sha256:aaa", "img/1.0-SNAPSHOT", "img/2.0-SNAPSHOT"),
	}

	propagatePlatformDecisions(decisions)

	assertAction(t, decisions, "img/sha256:aaa", cleaner.DELETE)
}

func TestPropagate_SharedDigest_SnapshotDeletedReleaseKept_PlatformKept(t *testing.T) {
	// sha256:aaa shared between a snapshot (DELETE) and a release (RECENT_VERSION).
	// Manifest lists get independent decisions per their rule.
	// Platform must be kept because release still references it.
	decisions := map[string]*cleaner.CleanupDecision{
		"img/1.0-SNAPSHOT": {CleanupAction: cleaner.DELETE, Artifact: artifactory.Metadata{Path: "img/1.0-SNAPSHOT"}},
		"img/1.0":          {CleanupAction: cleaner.RECENT_VERSION, Artifact: artifactory.Metadata{Path: "img/1.0"}},
		"img/sha256:aaa":   pendingPlatform("img", "sha256:aaa", "img/1.0-SNAPSHOT", "img/1.0"),
	}

	propagatePlatformDecisions(decisions)

	assertAction(t, decisions, "img/1.0-SNAPSHOT", cleaner.DELETE)
	assertAction(t, decisions, "img/1.0", cleaner.RECENT_VERSION)
	assertAction(t, decisions, "img/sha256:aaa", cleaner.MANIFEST_LIST_REF)
}

// ── applyManifestListDecisions ────────────────────────────────────────────────

var (
	snapshotRule = &cleaner.RuleSettings{
		Name:                       "snapshots",
		Pattern:                    `.*-SNAPSHOT.*`,
		RecentArtifactRetention:    3,
		LastDownloadedDays:         1,
		ArtifactLifetimeDays:       7,
		DeleteOrphanedManifestsList: true,
	}
)

func makeManifestIndex(entries map[string]artifactory.Metadata) map[string][]*artifactory.Metadata {
	idx := make(map[string][]*artifactory.Metadata)
	for sha, m := range entries {
		copy := m
		idx[sha] = append(idx[sha], &copy)
	}
	return idx
}

func makeDigestsByPath(entries map[string][]string) map[string][]string {
	return entries
}

func TestApplyManifestListDecisions_OrphanWithDeleteRule(t *testing.T) {
	// Manifest list with no platform manifests + deleteOrphanedManifestsList=true → DELETE_ORPHANED
	manifestLists := []artifactory.Metadata{
		{Path: "img/1.0-SNAPSHOT", Group: "img", Version: "1.0-SNAPSHOT"},
	}
	digestsByPath := makeDigestsByPath(map[string][]string{
		"img/1.0-SNAPSHOT": {"aaa"},
	})
	manifestIndex := makeManifestIndex(map[string]artifactory.Metadata{}) // empty — no manifests
	decisions := make(map[string]*cleaner.CleanupDecision)
	s := cleaner.TargetSettings{Rules: []cleaner.RuleSettings{*snapshotRule}}

	applyManifestListDecisions(manifestLists, digestsByPath, manifestIndex, decisions, &s)

	assertAction(t, decisions, "img/1.0-SNAPSHOT", cleaner.DELETE_ORPHANED)
}

func TestApplyManifestListDecisions_OrphanWithoutDeleteRule_KeepOrphaned(t *testing.T) {
	// Manifest list with no platform manifests + deleteOrphanedManifestsList=false → KEEP_ORPHANED
	manifestLists := []artifactory.Metadata{
		{Path: "img/1.0", Group: "img", Version: "1.0"},
	}
	digestsByPath := makeDigestsByPath(map[string][]string{
		"img/1.0": {"aaa"},
	})
	manifestIndex := makeManifestIndex(map[string]artifactory.Metadata{})
	decisions := make(map[string]*cleaner.CleanupDecision)
	releaseRuleCopy := *releaseRule // no DeleteOrphanedManifestsList
	s := cleaner.TargetSettings{Rules: []cleaner.RuleSettings{releaseRuleCopy}}

	applyManifestListDecisions(manifestLists, digestsByPath, manifestIndex, decisions, &s)

	assertAction(t, decisions, "img/1.0", cleaner.KEEP_ORPHANED)
}

func TestApplyManifestListDecisions_EffectiveDownloadFromPlatform(t *testing.T) {
	// Manifest list's effectiveDownload comes from platform manifests, not from its own stats.
	// Platform downloaded yesterday → DOWNLOADED_RECENTLY for lastDownloadedDays=90.
	yesterday := now.AddDate(0, 0, -1)
	oldCreated := now.AddDate(0, 0, -200)
	manifestLists := []artifactory.Metadata{
		{Path: "img/1.0", Group: "img", Version: "1.0", CreatedAt: &oldCreated},
	}
	digestsByPath := makeDigestsByPath(map[string][]string{
		"img/1.0": {"sha-amd64"},
	})
	manifestIndex := makeManifestIndex(map[string]artifactory.Metadata{
		"sha-amd64": {Path: "img/sha256:sha-amd64", Group: "img", Version: "sha256:sha-amd64", LastDownloadedAt: &yesterday},
	})
	decisions := make(map[string]*cleaner.CleanupDecision)
	s := cleaner.TargetSettings{Rules: []cleaner.RuleSettings{*releaseRule}}

	applyManifestListDecisions(manifestLists, digestsByPath, manifestIndex, decisions, &s)

	assertAction(t, decisions, "img/1.0", cleaner.DOWNLOADED_RECENTLY)
}

func TestApplyManifestListDecisions_SharedDigest_BothParentsLinked(t *testing.T) {
	// Same platform sha256 referenced by two manifest lists — both get linked as parents.
	oldCreated := now.AddDate(0, 0, -200)
	dl := now.AddDate(0, 0, -200)
	manifestLists := []artifactory.Metadata{
		{Path: "img/1.0-SNAPSHOT", Group: "img", Version: "1.0-SNAPSHOT", CreatedAt: &oldCreated},
		{Path: "img/master", Group: "img", Version: "master", CreatedAt: &oldCreated},
	}
	digestsByPath := makeDigestsByPath(map[string][]string{
		"img/1.0-SNAPSHOT": {"sha-arm64"},
		"img/master":       {"sha-arm64"},
	})
	manifestIndex := makeManifestIndex(map[string]artifactory.Metadata{
		"sha-arm64": {Path: "img/sha256:sha-arm64", Group: "img", Version: "sha256:sha-arm64", LastDownloadedAt: &dl},
	})
	decisions := make(map[string]*cleaner.CleanupDecision)
	s := cleaner.TargetSettings{UnmatchedAction: "keep", Rules: []cleaner.RuleSettings{*snapshotRule}}

	applyManifestListDecisions(manifestLists, digestsByPath, manifestIndex, decisions, &s)

	platform := decisions["img/sha256:sha-arm64"]
	if platform == nil {
		t.Fatal("platform decision not found")
	}
	if len(platform.Artifact.Parent) != 2 {
		t.Errorf("expected 2 parents, got %d: %v", len(platform.Artifact.Parent), platform.Artifact.Parent)
	}
}

// ── applyStandaloneDecisions ──────────────────────────────────────────────────

func TestApplyStandaloneDecisions_SkipsAlreadyDecided(t *testing.T) {
	// Platforms already in decisions (linked by manifest list) must not be overwritten.
	existing := &cleaner.CleanupDecision{CleanupAction: pending, Artifact: artifactory.Metadata{Path: "img/sha256:aaa", Version: "sha256:aaa"}}
	decisions := map[string]*cleaner.CleanupDecision{
		"img/sha256:aaa": existing,
	}
	m := artifactory.Metadata{Path: "img/sha256:aaa", SHA256: "aaa", Version: "sha256:aaa"}
	manifestIndex := map[string][]*artifactory.Metadata{"aaa": {&m}}
	s := cleaner.TargetSettings{}

	applyStandaloneDecisions(manifestIndex, decisions, &s)

	if decisions["img/sha256:aaa"] != existing {
		t.Error("existing platform decision was overwritten")
	}
}

func TestApplyStandaloneDecisions_Sha256Dir_DeleteOrphaned(t *testing.T) {
	// Unclaimed sha256 dir → DELETE_ORPHANED
	decisions := make(map[string]*cleaner.CleanupDecision)
	m := artifactory.Metadata{Path: "img/sha256:bbb", SHA256: "bbb", Version: "sha256:bbb", Group: "img"}
	manifestIndex := map[string][]*artifactory.Metadata{"bbb": {&m}}
	s := cleaner.TargetSettings{}

	applyStandaloneDecisions(manifestIndex, decisions, &s)

	assertAction(t, decisions, "img/sha256:bbb", cleaner.DELETE_ORPHANED)
}

func TestApplyStandaloneDecisions_NamedTag_AppliesRule(t *testing.T) {
	// Named standalone tag goes through makeDecision normally.
	oldCreated := now.AddDate(0, 0, -200)
	decisions := make(map[string]*cleaner.CleanupDecision)
	m := artifactory.Metadata{Path: "img/1.0", SHA256: "ccc", Version: "1.0", Group: "img", CreatedAt: &oldCreated}
	manifestIndex := map[string][]*artifactory.Metadata{"ccc": {&m}}
	s := cleaner.TargetSettings{Rules: []cleaner.RuleSettings{*releaseRule}}

	applyStandaloneDecisions(manifestIndex, decisions, &s)

	// Created 200 days ago, no download — should be pending (goes to retention)
	assertAction(t, decisions, "img/1.0", pending)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func assertAction(t *testing.T, decisions map[string]*cleaner.CleanupDecision, path string, want cleaner.CleanupAction) {
	t.Helper()
	d, ok := decisions[path]
	if !ok {
		t.Errorf("path %q not found in decisions", path)
		return
	}
	if d.CleanupAction != want {
		t.Errorf("path=%s: got %v, want %v", path, d.CleanupAction, want)
	}
}
