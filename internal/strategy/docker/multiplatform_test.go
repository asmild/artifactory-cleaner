package docker

import (
	"context"
	"testing"
	"time"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
	"github.com/asmild/artifactory-cleaner/internal/cleaner"
)

// ── buildVirtualArtifacts ─────────────────────────────────────────────────────

func TestBuildVirtualArtifacts_EffectiveDownloadFromPlatformStats(t *testing.T) {
	recentPull := time.Now().AddDate(0, 0, -2)
	manifestLists := []artifactory.Metadata{meta("img", "2.26.0", time.Now().AddDate(0, 0, -10))}
	digestMap := map[string][]string{"img/2.26.0": {"sha256:amd64", "sha256:arm64"}}
	checksumIndex := map[string][]artifactory.ManifestStat{
		"amd64": {{Path: "img/amd64-2.26.0", DownloadedAt: &recentPull}},
		"arm64": {{Path: "img/arm64-2.26.0", DownloadedAt: nil}},
	}

	result := buildVirtualArtifacts(manifestLists, digestMap, checksumIndex)

	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].Version != "2.26.0" {
		t.Errorf("Version = %q, want 2.26.0", result[0].Version)
	}
	if result[0].LastDownloadedAt == nil || !result[0].LastDownloadedAt.Equal(recentPull) {
		t.Errorf("LastDownloadedAt = %v, want %v", result[0].LastDownloadedAt, recentPull)
	}
}

func TestBuildVirtualArtifacts_NoPlatformDownloads_NilLastDownloaded(t *testing.T) {
	manifestLists := []artifactory.Metadata{meta("img", "1.0.0", time.Now().AddDate(0, 0, -30))}
	digestMap := map[string][]string{"img/1.0.0": {"sha256:abc"}}
	checksumIndex := map[string][]artifactory.ManifestStat{
		"abc": {{Path: "img/amd64-1.0.0", DownloadedAt: nil}},
	}

	result := buildVirtualArtifacts(manifestLists, digestMap, checksumIndex)

	if result[0].LastDownloadedAt != nil {
		t.Errorf("expected nil LastDownloadedAt, got %v", result[0].LastDownloadedAt)
	}
}

func TestBuildVirtualArtifacts_TakesMaxAcrossPlatforms(t *testing.T) {
	old := time.Now().AddDate(0, 0, -20)
	recent := time.Now().AddDate(0, 0, -1)
	manifestLists := []artifactory.Metadata{meta("img", "v1", time.Now())}
	digestMap := map[string][]string{"img/v1": {"sha256:a", "sha256:b"}}
	checksumIndex := map[string][]artifactory.ManifestStat{
		"a": {{Path: "img/amd64-v1", DownloadedAt: &old}},
		"b": {{Path: "img/arm64-v1", DownloadedAt: &recent}},
	}

	result := buildVirtualArtifacts(manifestLists, digestMap, checksumIndex)

	if result[0].LastDownloadedAt == nil || !result[0].LastDownloadedAt.Equal(recent) {
		t.Errorf("expected max download %v, got %v", recent, result[0].LastDownloadedAt)
	}
}

// ── addPlatformImageDecisions ─────────────────────────────────────────────────

func TestAddPlatformImageDecisions_KeptList_PlatformGetsManifestListRef(t *testing.T) {
	decisions := map[string][]cleaner.CleanupDecision{
		"img": {withAction(dec("img", "2.26.0", now()), cleaner.RECENT_VERSION)},
	}
	digestMap := map[string][]string{"img/2.26.0": {"sha256:abc"}}
	checksumIndex := map[string][]artifactory.ManifestStat{
		"abc": {{Path: "img/amd64-2.26.0"}},
	}

	addPlatformImageDecisions(decisions, digestMap, checksumIndex)

	expectVersion(t, decisions["img"], "sha256:abc", cleaner.MANIFEST_LIST_REF)
	expectVersion(t, decisions["img"], "amd64-2.26.0", cleaner.MANIFEST_LIST_REF)
}

func TestAddPlatformImageDecisions_DeletedList_PlatformGetsDelete(t *testing.T) {
	decisions := map[string][]cleaner.CleanupDecision{
		"img": {withAction(dec("img", "1.0.0", past()), cleaner.DELETE)},
	}
	digestMap := map[string][]string{"img/1.0.0": {"sha256:def"}}
	checksumIndex := map[string][]artifactory.ManifestStat{
		"def": {{Path: "img/arm64-1.0.0"}},
	}

	addPlatformImageDecisions(decisions, digestMap, checksumIndex)

	expectVersion(t, decisions["img"], "sha256:def", cleaner.DELETE)
	expectVersion(t, decisions["img"], "arm64-1.0.0", cleaner.DELETE)
}

func TestAddPlatformImageDecisions_ReturnsHandledPaths(t *testing.T) {
	decisions := map[string][]cleaner.CleanupDecision{
		"img": {withAction(dec("img", "v1", now()), cleaner.RECENT_VERSION)},
	}
	handled := addPlatformImageDecisions(
		decisions,
		map[string][]string{"img/v1": {"sha256:aaa"}},
		map[string][]artifactory.ManifestStat{"aaa": {{Path: "img/amd64-v1"}}},
	)

	if !handled["img/sha256:aaa"] {
		t.Error("sha256 dir should be in handledPaths")
	}
	if !handled["img/amd64-v1"] {
		t.Error("named arch tag should be in handledPaths")
	}
}

// ── planMultiPlatform integration scenarios ───────────────────────────────────

// RecentlyPulled: platform image downloaded 2 days ago → DOWNLOADED_RECENTLY → kept.
func TestPlanMultiPlatform_RecentlyPulled_ManifestListKept(t *testing.T) {
	recentPull := time.Now().AddDate(0, 0, -2)
	repo := &mockRepo{
		artifacts: []artifactory.Metadata{meta("img", "v1.0.0", time.Now().AddDate(0, 0, -30))},
		digests:   map[string][]string{"img/v1.0.0": {"sha256:amd64"}},
		checksums: map[string][]artifactory.ManifestStat{
			"amd64": {{Path: "img/amd64-v1.0.0", DownloadedAt: &recentPull}},
		},
	}
	s := cleaner.TargetSettings{
		Name: "docker-local",
		Rules: []cleaner.RuleSettings{{
			Name: "releases", Pattern: `\d+\.\d+\.\d+`,
			RecentArtifactRetention: 0, LastDownloadedDays: 7,
		}},
		UnmatchedAction: "delete",
	}

	dm, _, err := planMultiPlatform(context.Background(), repo, s)
	if err != nil {
		t.Fatal(err)
	}
	find := findAction(t, dm["img"])
	if a := find("v1.0.0"); a != cleaner.DOWNLOADED_RECENTLY {
		t.Errorf("v1.0.0: got %s, want DOWNLOADED_RECENTLY", cleaner.CleanupActionStrings[a])
	}
	if a := find("sha256:amd64"); a != cleaner.MANIFEST_LIST_REF {
		t.Errorf("sha256:amd64: got %s, want MANIFEST_LIST_REF", cleaner.CleanupActionStrings[a])
	}
}

// NeverPulled_OldImage: no pulls, old image → DELETE.
func TestPlanMultiPlatform_NeverPulled_OldImage_Deleted(t *testing.T) {
	repo := &mockRepo{
		artifacts: []artifactory.Metadata{meta("img", "v0.1.0", time.Now().AddDate(0, 0, -90))},
		digests:   map[string][]string{"img/v0.1.0": {"sha256:amd64"}},
		checksums: map[string][]artifactory.ManifestStat{
			"amd64": {{Path: "img/amd64-v0.1.0", DownloadedAt: nil}},
		},
	}
	s := cleaner.TargetSettings{
		Name: "docker-local",
		Rules: []cleaner.RuleSettings{{
			Name: "releases", Pattern: `\d+\.\d+\.\d+`,
			RecentArtifactRetention: 0, LastDownloadedDays: 7,
		}},
		UnmatchedAction: "delete",
	}

	dm, _, err := planMultiPlatform(context.Background(), repo, s)
	if err != nil {
		t.Fatal(err)
	}
	find := findAction(t, dm["img"])
	if a := find("v0.1.0"); a != cleaner.DELETE {
		t.Errorf("v0.1.0: got %s, want DELETE", cleaner.CleanupActionStrings[a])
	}
	if a := find("sha256:amd64"); a != cleaner.DELETE {
		t.Errorf("sha256:amd64: got %s, want DELETE", cleaner.CleanupActionStrings[a])
	}
}

// ContaminatedManifestStatIgnored: manifest list stat.downloaded is "today"
// (contaminated by cleaner), but platform image was pulled 60 days ago → DELETE.
func TestPlanMultiPlatform_ContaminatedManifestStatIgnored(t *testing.T) {
	oldPull := time.Now().AddDate(0, 0, -60)
	today := time.Now()
	repo := &mockRepo{
		artifacts: []artifactory.Metadata{
			func() artifactory.Metadata {
				m := meta("img", "v1.0.0", time.Now().AddDate(0, 0, -30))
				m.LastDownloadedAt = &today // contaminated — must be ignored
				return m
			}(),
		},
		digests: map[string][]string{"img/v1.0.0": {"sha256:amd64"}},
		checksums: map[string][]artifactory.ManifestStat{
			"amd64": {{Path: "img/amd64-v1.0.0", DownloadedAt: &oldPull}},
		},
	}
	s := cleaner.TargetSettings{
		Name: "docker-local",
		Rules: []cleaner.RuleSettings{{
			Name: "releases", Pattern: `\d+\.\d+\.\d+`,
			RecentArtifactRetention: 0, LastDownloadedDays: 7,
		}},
		UnmatchedAction: "delete",
	}

	dm, _, err := planMultiPlatform(context.Background(), repo, s)
	if err != nil {
		t.Fatal(err)
	}
	// Contaminated stat must be ignored; platform pulled 60 days ago → DELETE.
	if a := findAction(t, dm["img"])("v1.0.0"); a != cleaner.DELETE {
		t.Errorf("v1.0.0: got %s, want DELETE (contaminated stat ignored)", cleaner.CleanupActionStrings[a])
	}
}

// OnePlatformRecentlyPulled: manifest list is old/outside retention, but one
// platform image was pulled 3 days ago → whole list KEPT (DOWNLOADED_RECENTLY).
// arm64 was never pulled — doesn't matter, amd64 saves the group.
func TestPlanMultiPlatform_OnePlatformRecentlyPulled_WholeListKept(t *testing.T) {
	recentPull := time.Now().AddDate(0, 0, -3)
	repo := &mockRepo{
		artifacts: []artifactory.Metadata{
			meta("my-service", "1.5.0", time.Now().AddDate(0, 0, -30)),
		},
		digests: map[string][]string{
			"my-service/1.5.0": {"sha256:amd64digest", "sha256:arm64digest"},
		},
		checksums: map[string][]artifactory.ManifestStat{
			"amd64digest": {{Path: "my-service/amd64-1.5.0", DownloadedAt: &recentPull}},
			"arm64digest": {{Path: "my-service/arm64-1.5.0", DownloadedAt: nil}},
		},
	}
	s := cleaner.TargetSettings{
		Name: "docker-local",
		Rules: []cleaner.RuleSettings{{
			Name: "releases", Pattern: `\d+\.\d+\.\d+`,
			RecentArtifactRetention: 0, LastDownloadedDays: 7,
		}},
	}

	dm, _, err := planMultiPlatform(context.Background(), repo, s)
	if err != nil {
		t.Fatal(err)
	}
	find := findAction(t, dm["my-service"])
	if a := find("1.5.0"); a != cleaner.DOWNLOADED_RECENTLY {
		t.Errorf("1.5.0: got %s, want DOWNLOADED_RECENTLY (saved by amd64 pull)", cleaner.CleanupActionStrings[a])
	}
	// Both platforms kept — even arm64 which was never pulled.
	if a := find("sha256:amd64digest"); a != cleaner.MANIFEST_LIST_REF {
		t.Errorf("sha256:amd64digest: got %s, want MANIFEST_LIST_REF", cleaner.CleanupActionStrings[a])
	}
	if a := find("sha256:arm64digest"); a != cleaner.MANIFEST_LIST_REF {
		t.Errorf("sha256:arm64digest: got %s, want MANIFEST_LIST_REF (kept with parent)", cleaner.CleanupActionStrings[a])
	}
}

// AllPlatformsRecentlyPulled: both arm64 and amd64 pulled recently → KEPT.
func TestPlanMultiPlatform_AllPlatformsRecentlyPulled_ListKept(t *testing.T) {
	pull := time.Now().AddDate(0, 0, -1)
	repo := &mockRepo{
		artifacts: []artifactory.Metadata{meta("img", "v2.0.0", time.Now().AddDate(0, 0, -20))},
		digests:   map[string][]string{"img/v2.0.0": {"sha256:a", "sha256:b"}},
		checksums: map[string][]artifactory.ManifestStat{
			"a": {{Path: "img/amd64-v2.0.0", DownloadedAt: &pull}},
			"b": {{Path: "img/arm64-v2.0.0", DownloadedAt: &pull}},
		},
	}
	s := cleaner.TargetSettings{Name: "docker-local", Rules: []cleaner.RuleSettings{{
		Name: "r", Pattern: `.*`, RecentArtifactRetention: 0, LastDownloadedDays: 7,
	}}}

	dm, _, err := planMultiPlatform(context.Background(), repo, s)
	if err != nil {
		t.Fatal(err)
	}
	if a := findAction(t, dm["img"])("v2.0.0"); a != cleaner.DOWNLOADED_RECENTLY {
		t.Errorf("got %s, want DOWNLOADED_RECENTLY", cleaner.CleanupActionStrings[a])
	}
}

// AllPlatformsNeverPulled_OldImage: nothing pulled, outside retention → DELETE.
func TestPlanMultiPlatform_AllPlatformsNeverPulled_OldImage_Deleted(t *testing.T) {
	repo := &mockRepo{
		artifacts: []artifactory.Metadata{meta("img", "v0.1.0", time.Now().AddDate(0, 0, -90))},
		digests:   map[string][]string{"img/v0.1.0": {"sha256:a", "sha256:b"}},
		checksums: map[string][]artifactory.ManifestStat{
			"a": {{Path: "img/amd64-v0.1.0", DownloadedAt: nil}},
			"b": {{Path: "img/arm64-v0.1.0", DownloadedAt: nil}},
		},
	}
	s := cleaner.TargetSettings{Name: "docker-local", Rules: []cleaner.RuleSettings{{
		Name: "r", Pattern: `.*`, RecentArtifactRetention: 0, LastDownloadedDays: 7,
	}}, UnmatchedAction: "delete"}

	dm, _, err := planMultiPlatform(context.Background(), repo, s)
	if err != nil {
		t.Fatal(err)
	}
	find := findAction(t, dm["img"])
	if a := find("v0.1.0"); a != cleaner.DELETE {
		t.Errorf("got %s, want DELETE", cleaner.CleanupActionStrings[a])
	}
	if a := find("sha256:a"); a != cleaner.DELETE {
		t.Errorf("sha256:a got %s, want DELETE", cleaner.CleanupActionStrings[a])
	}
}

// RetentionCountSavesListRegardlessOfPullStats: v2.0.0 is within retention=1
// even though no platform was pulled → RECENT_VERSION.
func TestPlanMultiPlatform_RetentionCountSavesList(t *testing.T) {
	repo := &mockRepo{
		artifacts: []artifactory.Metadata{
			meta("img", "v2.0.0", time.Now().Add(-1*time.Hour)),
			meta("img", "v1.0.0", time.Now().AddDate(0, 0, -30)),
		},
		digests: map[string][]string{
			"img/v2.0.0": {"sha256:new"},
			"img/v1.0.0": {"sha256:old"},
		},
		checksums: map[string][]artifactory.ManifestStat{
			"new": {{Path: "img/amd64-v2.0.0", DownloadedAt: nil}},
			"old": {{Path: "img/amd64-v1.0.0", DownloadedAt: nil}},
		},
	}
	s := cleaner.TargetSettings{Name: "docker-local", Rules: []cleaner.RuleSettings{{
		Name: "releases", Pattern: `\d+\.\d+\.\d+`,
		RecentArtifactRetention: 1, LastDownloadedDays: 0,
	}}, UnmatchedAction: "delete"}

	dm, _, err := planMultiPlatform(context.Background(), repo, s)
	if err != nil {
		t.Fatal(err)
	}
	find := findAction(t, dm["img"])
	if a := find("v2.0.0"); a != cleaner.RECENT_VERSION {
		t.Errorf("v2.0.0: got %s, want RECENT_VERSION", cleaner.CleanupActionStrings[a])
	}
	if a := find("v1.0.0"); a != cleaner.DELETE {
		t.Errorf("v1.0.0: got %s, want DELETE", cleaner.CleanupActionStrings[a])
	}
}

// ArtifactLifetimeDays: newly created image, no pulls yet → CREATED_RECENTLY.
func TestPlanMultiPlatform_NewImageNoPulls_ProtectedByLifetimeDays(t *testing.T) {
	repo := &mockRepo{
		artifacts: []artifactory.Metadata{meta("img", "v3.0.0", time.Now().AddDate(0, 0, -2))},
		digests:   map[string][]string{"img/v3.0.0": {"sha256:x"}},
		checksums: map[string][]artifactory.ManifestStat{
			"x": {{Path: "img/amd64-v3.0.0", DownloadedAt: nil}},
		},
	}
	s := cleaner.TargetSettings{Name: "docker-local", Rules: []cleaner.RuleSettings{{
		Name: "releases", Pattern: `\d+\.\d+\.\d+`,
		RecentArtifactRetention: 0, LastDownloadedDays: 0,
		ArtifactLifetimeDays: 7, // brand new image — grace period
	}}, UnmatchedAction: "delete"}

	dm, _, err := planMultiPlatform(context.Background(), repo, s)
	if err != nil {
		t.Fatal(err)
	}
	if a := findAction(t, dm["img"])("v3.0.0"); a != cleaner.CREATED_RECENTLY {
		t.Errorf("got %s, want CREATED_RECENTLY", cleaner.CleanupActionStrings[a])
	}
}

// ProtectedVersion: in protectedVersions → PROTECTED regardless of pull stats.
func TestPlanMultiPlatform_ProtectedVersion_NeverDeleted(t *testing.T) {
	repo := &mockRepo{
		artifacts: []artifactory.Metadata{meta("img", "latest", time.Now().AddDate(0, 0, -100))},
		digests:   map[string][]string{"img/latest": {"sha256:l"}},
		checksums: map[string][]artifactory.ManifestStat{
			"l": {{Path: "img/amd64-latest", DownloadedAt: nil}},
		},
	}
	s := cleaner.TargetSettings{
		Name:              "docker-local",
		ProtectedVersions: []string{"latest"},
		Rules: []cleaner.RuleSettings{{
			Name: "r", Pattern: `.*`,
			RecentArtifactRetention: 0, LastDownloadedDays: 0,
		}},
		UnmatchedAction: "delete",
	}

	dm, _, err := planMultiPlatform(context.Background(), repo, s)
	if err != nil {
		t.Fatal(err)
	}
	if a := findAction(t, dm["img"])("latest"); a != cleaner.PROTECTED {
		t.Errorf("got %s, want PROTECTED", cleaner.CleanupActionStrings[a])
	}
}

// MultipleVersions_MixedPullHistory: v2 pulled recently (kept), v1 not pulled (deleted).
func TestPlanMultiPlatform_MultipleVersions_MixedPullHistory(t *testing.T) {
	recentPull := time.Now().AddDate(0, 0, -2)
	repo := &mockRepo{
		artifacts: []artifactory.Metadata{
			meta("svc", "v2.0.0", time.Now().Add(-1*time.Hour)),
			meta("svc", "v1.0.0", time.Now().AddDate(0, 0, -60)),
		},
		digests: map[string][]string{
			"svc/v2.0.0": {"sha256:v2amd", "sha256:v2arm"},
			"svc/v1.0.0": {"sha256:v1amd", "sha256:v1arm"},
		},
		checksums: map[string][]artifactory.ManifestStat{
			"v2amd": {{Path: "svc/amd64-v2.0.0", DownloadedAt: &recentPull}},
			"v2arm": {{Path: "svc/arm64-v2.0.0", DownloadedAt: nil}},
			"v1amd": {{Path: "svc/amd64-v1.0.0", DownloadedAt: nil}},
			"v1arm": {{Path: "svc/arm64-v1.0.0", DownloadedAt: nil}},
		},
	}
	s := cleaner.TargetSettings{Name: "docker-local", Rules: []cleaner.RuleSettings{{
		Name: "releases", Pattern: `\d+\.\d+\.\d+`,
		RecentArtifactRetention: 0, LastDownloadedDays: 7,
	}}, UnmatchedAction: "delete"}

	dm, _, err := planMultiPlatform(context.Background(), repo, s)
	if err != nil {
		t.Fatal(err)
	}
	find := findAction(t, dm["svc"])
	if a := find("v2.0.0"); a != cleaner.DOWNLOADED_RECENTLY {
		t.Errorf("v2.0.0: got %s, want DOWNLOADED_RECENTLY", cleaner.CleanupActionStrings[a])
	}
	if a := find("v1.0.0"); a != cleaner.DELETE {
		t.Errorf("v1.0.0: got %s, want DELETE", cleaner.CleanupActionStrings[a])
	}
	if a := find("sha256:v2amd"); a != cleaner.MANIFEST_LIST_REF {
		t.Errorf("sha256:v2amd: got %s, want MANIFEST_LIST_REF", cleaner.CleanupActionStrings[a])
	}
	if a := find("sha256:v1arm"); a != cleaner.DELETE {
		t.Errorf("sha256:v1arm: got %s, want DELETE", cleaner.CleanupActionStrings[a])
	}
}

// NoPlatformInChecksumIndex: manifest list references a digest with no matching
// manifest.json in the repo (e.g. purged) → falls back to created date.
func TestPlanMultiPlatform_NoPlatformInChecksumIndex_FallsBackToCreated(t *testing.T) {
	repo := &mockRepo{
		artifacts: []artifactory.Metadata{meta("img", "v1.0.0", time.Now().AddDate(0, 0, -50))},
		digests:   map[string][]string{"img/v1.0.0": {"sha256:missing"}},
		checksums: map[string][]artifactory.ManifestStat{}, // no matching manifest.json
	}
	s := cleaner.TargetSettings{Name: "docker-local", Rules: []cleaner.RuleSettings{{
		Name: "releases", Pattern: `\d+\.\d+\.\d+`,
		RecentArtifactRetention: 0, LastDownloadedDays: 7,
	}}, UnmatchedAction: "delete"}

	dm, _, err := planMultiPlatform(context.Background(), repo, s)
	if err != nil {
		t.Fatal(err)
	}
	// No platform stats → LastDownloadedAt=nil → MakeDecisions falls back to
	// CreatedAt (50 days ago) → outside lastDownloadedDays=7 → DELETE.
	if a := findAction(t, dm["img"])("v1.0.0"); a != cleaner.DELETE {
		t.Errorf("got %s, want DELETE (no platform stats, created 50 days ago)", cleaner.CleanupActionStrings[a])
	}
}

// MultipleImages_IndependentGroups: pull history of img-a doesn't affect img-b.
func TestPlanMultiPlatform_MultipleImages_IndependentGroups(t *testing.T) {
	recentPull := time.Now().AddDate(0, 0, -1)
	repo := &mockRepo{
		artifacts: []artifactory.Metadata{
			meta("img-a", "v1.0.0", time.Now().AddDate(0, 0, -30)),
			meta("img-b", "v1.0.0", time.Now().AddDate(0, 0, -30)),
		},
		digests: map[string][]string{
			"img-a/v1.0.0": {"sha256:a"},
			"img-b/v1.0.0": {"sha256:b"},
		},
		checksums: map[string][]artifactory.ManifestStat{
			"a": {{Path: "img-a/amd64-v1.0.0", DownloadedAt: &recentPull}}, // a pulled
			"b": {{Path: "img-b/amd64-v1.0.0", DownloadedAt: nil}},          // b not pulled
		},
	}
	s := cleaner.TargetSettings{Name: "docker-local", Rules: []cleaner.RuleSettings{{
		Name: "r", Pattern: `.*`, RecentArtifactRetention: 0, LastDownloadedDays: 7,
	}}, UnmatchedAction: "delete"}

	dm, _, err := planMultiPlatform(context.Background(), repo, s)
	if err != nil {
		t.Fatal(err)
	}
	if a := findAction(t, dm["img-a"])("v1.0.0"); a != cleaner.DOWNLOADED_RECENTLY {
		t.Errorf("img-a: got %s, want DOWNLOADED_RECENTLY", cleaner.CleanupActionStrings[a])
	}
	if a := findAction(t, dm["img-b"])("v1.0.0"); a != cleaner.DELETE {
		t.Errorf("img-b: got %s, want DELETE", cleaner.CleanupActionStrings[a])
	}
}

// PatternMatchUsesManifestListTag: amd64-2.0.0-SNAPSHOT is classified as
// MANIFEST_LIST_REF because its effective_version "2.0.0-SNAPSHOT" matches the rule.
func TestPlanMultiPlatform_PatternMatchUsesManifestListTag(t *testing.T) {
	repo := &mockRepo{
		artifacts: []artifactory.Metadata{
			meta("img", "2.0.0-SNAPSHOT", time.Now().Add(-1*time.Hour)),
			meta("img", "1.0.0-SNAPSHOT", time.Now().AddDate(0, 0, -10)),
		},
		digests: map[string][]string{
			"img/2.0.0-SNAPSHOT": {"sha256:new"},
			"img/1.0.0-SNAPSHOT": {"sha256:old"},
		},
		checksums: map[string][]artifactory.ManifestStat{
			"new": {{Path: "img/amd64-2.0.0-SNAPSHOT"}},
			"old": {{Path: "img/amd64-1.0.0-SNAPSHOT"}},
		},
	}
	s := cleaner.TargetSettings{
		Name: "docker-local",
		Rules: []cleaner.RuleSettings{{
			Name: "snapshots", Pattern: `.*-SNAPSHOT`,
			RecentArtifactRetention: 1, LastDownloadedDays: 0,
		}},
		UnmatchedAction: "delete",
	}

	dm, _, err := planMultiPlatform(context.Background(), repo, s)
	if err != nil {
		t.Fatal(err)
	}
	find := findAction(t, dm["img"])
	if a := find("2.0.0-SNAPSHOT"); a != cleaner.RECENT_VERSION {
		t.Errorf("2.0.0-SNAPSHOT: got %s, want RECENT_VERSION", cleaner.CleanupActionStrings[a])
	}
	if a := find("1.0.0-SNAPSHOT"); a != cleaner.DELETE {
		t.Errorf("1.0.0-SNAPSHOT: got %s, want DELETE", cleaner.CleanupActionStrings[a])
	}
	if a := find("amd64-2.0.0-SNAPSHOT"); a != cleaner.MANIFEST_LIST_REF {
		t.Errorf("amd64-2.0.0-SNAPSHOT: got %s, want MANIFEST_LIST_REF", cleaner.CleanupActionStrings[a])
	}
	if a := find("amd64-1.0.0-SNAPSHOT"); a != cleaner.DELETE {
		t.Errorf("amd64-1.0.0-SNAPSHOT: got %s, want DELETE", cleaner.CleanupActionStrings[a])
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func now() time.Time  { return time.Now() }
func past() time.Time { return time.Now().AddDate(-2, 0, 0) }

func meta(group, version string, created time.Time) artifactory.Metadata {
	return artifactory.Metadata{Path: group + "/" + version, Group: group, Version: version, Size: 531, CreatedAt: &created}
}

func dec(group, version string, created time.Time) cleaner.CleanupDecision {
	return cleaner.CleanupDecision{Artifact: meta(group, version, created)}
}

func withAction(d cleaner.CleanupDecision, a cleaner.CleanupAction) cleaner.CleanupDecision {
	d.CleanupAction = a
	return d
}

func expectVersion(t *testing.T, decisions []cleaner.CleanupDecision, version string, want cleaner.CleanupAction) {
	t.Helper()
	for _, d := range decisions {
		if d.Artifact.Version == version {
			if d.CleanupAction != want {
				t.Errorf("version %q: got %s, want %s", version,
					cleaner.CleanupActionStrings[d.CleanupAction], cleaner.CleanupActionStrings[want])
			}
			return
		}
	}
	t.Errorf("version %q not found", version)
}

func findAction(t *testing.T, decisions []cleaner.CleanupDecision) func(string) cleaner.CleanupAction {
	t.Helper()
	return func(version string) cleaner.CleanupAction {
		for _, d := range decisions {
			if d.Artifact.Version == version {
				return d.CleanupAction
			}
		}
		t.Fatalf("version %q not found", version)
		return 0
	}
}

type mockRepo struct {
	artifacts []artifactory.Metadata
	fetchErr  error
	digests   map[string][]string
	errors    map[string]error
	checksums map[string][]artifactory.ManifestStat
}

func (m *mockRepo) FindArtifacts(_ context.Context, _ artifactory.ArtifactFilter) ([]artifactory.Metadata, error) {
	return m.artifacts, m.fetchErr
}
func (m *mockRepo) GetManifestListDigests(_ context.Context, _ string, path string) ([]string, error) {
	if err, ok := m.errors[path]; ok {
		return nil, err
	}
	return m.digests[path], nil
}
func (m *mockRepo) FindManifestChecksums(_ context.Context, _ string) (map[string][]artifactory.ManifestStat, error) {
	if m.checksums != nil {
		return m.checksums, nil
	}
	return map[string][]artifactory.ManifestStat{}, nil
}
func (m *mockRepo) FetchDirectorySize(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}
func (m *mockRepo) DeletePath(_ context.Context, _, _ string) error { return nil }

var _ artifactory.Repository = (*artifactory.Client)(nil)
// already appended - will be deduped
