package docker

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
	"github.com/asmild/artifactory-cleaner/internal/cleaner"
)



func isProtected(group, version string, settings *cleaner.TargetSettings) bool {
	return slices.Contains(settings.ProtectedVersions, version) ||
		groupIsProtected(group, settings.ProtectedGroups)
}

// groupIsProtected returns true if the group exactly matches any entry in the
// protected list, or if the group is a sub-group of any entry (prefix match).
func groupIsProtected(group string, protectedGroups []string) bool {
	for _, entry := range protectedGroups {
		if group == entry || strings.HasPrefix(group, entry+"/") {
			return true
		}
	}
	return false
}

// isWhitelisted returns true if the group or version is explicitly whitelisted in the rule.
func isWhitelisted(group, version string, rule *cleaner.RuleSettings) bool {
	return slices.Contains(rule.WhitelistedVersions, version) ||
		slices.Contains(rule.WhitelistedGroups, group) ||
		slices.Contains(rule.WhitelistedArtifacts, group+"@"+version)
}

// isCreatedRecently returns true if the artifact was created within the last N days
func isCreatedRecently(createdAt *time.Time, rule *cleaner.RuleSettings) bool {
	if createdAt == nil || rule.ArtifactLifetimeDays == 0 {
		return false
	}
	return createdAt.After(time.Now().AddDate(0, 0, -rule.ArtifactLifetimeDays))
}

// isDownloadedRecently returns true if the artifact was pulled within the last N days
func isDownloadedRecently(downloadedAt *time.Time, rule *cleaner.RuleSettings) bool {
	if downloadedAt == nil || rule.LastDownloadedDays == 0 {
		return false
	}
	return downloadedAt.After(time.Now().AddDate(0, 0, -rule.LastDownloadedDays))
}

// resolveRetentionCounts resolves all pending decisions by grouping them per image,
// sorting newest-first, and assigning RECENT_VERSION slots up to recentArtifactRetention.
// Both manifest lists and standalones are in the same map so slots are counted across both types.
func resolveRetentionCounts(decisions map[string]*cleaner.CleanupDecision) {
	pendingByGroup := make(map[string][]*cleaner.CleanupDecision)

	// Group all pending decisions by artifact group (image name)
	for _, d := range decisions {
		if d.CleanupAction == pending && len(d.Artifact.Parent) == 0 {
			pendingByGroup[d.Artifact.Group] = append(pendingByGroup[d.Artifact.Group], d)
		}
	}

	retentionCounters := make(map[string]int) // "group/ruleName" → slots used

	for _, entries := range pendingByGroup {
		sort.Slice(entries, func(i, j int) bool {
			ci, cj := entries[i].Artifact.CreatedAt, entries[j].Artifact.CreatedAt
			if ci == nil {
				return false
			}
			if cj == nil {
				return true
			}
			return ci.After(*cj)
		})
		for _, d := range entries {
			if d.RuleSettings == nil {
				continue
			}
			counterKey := d.Artifact.Group + "/" + d.RuleSettings.Name
			if retentionCounters[counterKey] < d.RuleSettings.RecentArtifactRetention {
				retentionCounters[counterKey]++
				d.CleanupAction = cleaner.RECENT_VERSION
			} else {
				d.CleanupAction = cleaner.DELETE
			}
		}
	}
}

func makeDecision(artifact *artifactory.Metadata, rule *cleaner.RuleSettings, settings *cleaner.TargetSettings) cleaner.CleanupAction {
	switch {
	case isProtected(artifact.Group, artifact.Version, settings):
		return cleaner.PROTECTED
	case rule == nil:
		if settings.UnmatchedAction == "delete" {
			return cleaner.DELETE
		}
		return cleaner.UNMATCHED_KEEP
	case isWhitelisted(artifact.Group, artifact.Version, rule):
		return cleaner.WHITELISTED
	case isCreatedRecently(artifact.CreatedAt, rule):
		return cleaner.CREATED_RECENTLY
	case isDownloadedRecently(artifact.LastDownloadedAt, rule):
		return cleaner.DOWNLOADED_RECENTLY
	default:
		return pending
	}
}

// propagatePlatformDecisions sets final action on platform manifests based on their parents.
// If any parent is not DELETE — platform gets MANIFEST_LIST_REF (KEEP wins).
func propagatePlatformDecisions(decisions map[string]*cleaner.CleanupDecision) {
	for _, pd := range decisions {
		if len(pd.Artifact.Parent) == 0 {
			continue
		}
		effectiveAction := cleaner.DELETE
		for _, parentPath := range pd.Artifact.Parent {
			parent := decisions[parentPath]
			if parent != nil && parent.CleanupAction != cleaner.DELETE {
				effectiveAction = cleaner.MANIFEST_LIST_REF
				break
			}
		}
		pd.CleanupAction = effectiveAction
	}
}

func applyStandaloneDecisions(
	manifestIndex map[string][]*artifactory.Metadata,
	decisions map[string]*cleaner.CleanupDecision,
	settings *cleaner.TargetSettings,
) {
	for _, manifests := range manifestIndex {
		for _, m := range manifests {
			if decisions[m.Path] != nil {
				continue
			}
			md := &cleaner.CleanupDecision{Artifact: *m}
			if strings.HasPrefix(m.Version, "sha256:") {
				md.CleanupAction = cleaner.DELETE_ORPHANED
				decisions[m.Path] = md
				continue
			}
			rule := settings.MatchingRule(m.Version)
			md.RuleSettings = rule
			md.CleanupAction = makeDecision(m, rule, settings)
			decisions[m.Path] = md
		}
	}
}

func applyManifestListDecisions(
	manifestLists []artifactory.Metadata,
	digestsByPath map[string][]string,
	manifestIndex map[string][]*artifactory.Metadata,
	decisions map[string]*cleaner.CleanupDecision,
	settings *cleaner.TargetSettings,
) {
	for _, ml := range manifestLists {
		imageDigests := digestsByPath[ml.Path]
		foundCount, effectiveDownloadedAt := computeEffectiveDownload(imageDigests, manifestIndex)
		rule := settings.MatchingRule(ml.Version)

		var action cleaner.CleanupAction
		if foundCount == 0 {
			if rule != nil && rule.DeleteOrphanedManifestsList {
				action = cleaner.DELETE_ORPHANED
			} else {
				action = cleaner.KEEP_ORPHANED
			}
		} else {
			ml.LastDownloadedAt = effectiveDownloadedAt
			action = makeDecision(&ml, rule, settings)
		}

		mld := &cleaner.CleanupDecision{
			Artifact:      ml,
			RuleSettings:  rule,
			CleanupAction: action,
		}

		linkPlatformDecisions(imageDigests, manifestIndex, mld, decisions)
		decisions[ml.Path] = mld
	}
}
