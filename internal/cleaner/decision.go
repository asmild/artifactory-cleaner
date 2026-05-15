package cleaner

import (
	"regexp"
	"slices"
	"sort"
	"time"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
)

// MakeDecisions applies the target's rules to a grouped decision map in-place.
//
// Decision order per artifact (sorted newest-first within each group):
//  1. protectedGroups  → PROTECTED
//  2. protectedVersions → PROTECTED
//  3. First rule whose pattern matches:
//     a. rule whitelist → WHITELISTED
//     b. rule retention count (per rule, per group) → RECENT_VERSION
//     c. rule download recency → DOWNLOADED_RECENTLY
//     d. else → DELETE
//  4. No rule matched → UNMATCHED_KEEP or DELETE (per unmatchedAction)
//
// Retention counts are tracked independently per rule within each group, so
// snapshots and releases keep their own counters regardless of interleaving.
func MakeDecisions(decisions map[string][]CleanupDecision, settings TargetSettings) CleanupStatistics {
	// Pre-compile patterns and recency thresholds once.
	type compiledRule struct {
		RuleSettings
		re               *regexp.Regexp
		threshold        time.Time // lastDownloadedDays cutoff
		lifetimeThreshold time.Time // artifactLifetimeDays cutoff (zero = disabled)
	}
	now := time.Now()
	compiled := make([]compiledRule, 0, len(settings.Rules))
	for _, r := range settings.Rules {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			continue // invalid pattern already warned by ValidateSettings
		}
		var lifetimeThreshold time.Time
		if r.ArtifactLifetimeDays > 0 {
			lifetimeThreshold = now.AddDate(0, 0, -r.ArtifactLifetimeDays)
		}
		compiled = append(compiled, compiledRule{
			RuleSettings:      r,
			re:                re,
			threshold:         now.AddDate(0, 0, -r.LastDownloadedDays),
			lifetimeThreshold: lifetimeThreshold,
		})
	}

	var stats CleanupStatistics

	for group, groupDecisions := range decisions {
		groupIsProtected := slices.Contains(settings.ProtectedGroups, group)
		ruleCounts := make(map[string]int) // rule.Name → RECENT_VERSION slots used

		for i := range groupDecisions {
			d := &groupDecisions[i]
			meta := d.Artifact
			stats.TotalArtifacts++
			stats.TotalSize += meta.Size

			// 1 & 2. Target-level protection — checked before any rule.
			if groupIsProtected || slices.Contains(settings.ProtectedVersions, meta.Version) {
				d.CleanupAction = PROTECTED
				continue
			}

			// 3. Walk rules in order; first pattern match wins.
			matched := false
			for _, rule := range compiled {
				if !rule.re.MatchString(meta.Version) {
					continue
				}
				matched = true

				// Rule-scoped whitelists (only reachable when pattern matched).
				if slices.Contains(rule.WhitelistedGroups, group) ||
					slices.Contains(rule.WhitelistedVersions, meta.Version) ||
					slices.Contains(rule.WhitelistedArtifacts, group+"@"+meta.Version) {
					d.CleanupAction = WHITELISTED
					stats.ArtifactsWhitelisted++
					break
				}

				// Retention count (independent per rule per group).
				if ruleCounts[rule.Name] < rule.RecentArtifactRetention {
					ruleCounts[rule.Name]++
					d.CleanupAction = RECENT_VERSION
					break
				}

				// Grace period: keep anything created within artifactLifetimeDays,
				// regardless of whether it has been downloaded yet.
				if !rule.lifetimeThreshold.IsZero() &&
					meta.CreatedAt != nil && meta.CreatedAt.After(rule.lifetimeThreshold) {
					d.CleanupAction = CREATED_RECENTLY
					break
				}

				// Download recency.
				lastUsed := meta.LastDownloadedAt
				if lastUsed == nil {
					lastUsed = meta.CreatedAt
				}
				if lastUsed != nil && lastUsed.After(rule.threshold) {
					d.CleanupAction = DOWNLOADED_RECENTLY
					break
				}

				d.CleanupAction = DELETE
				stats.ArtifactsForDeletion++
				stats.TotalSizeForDeletion += meta.Size
				break
			}

			// 4. No rule matched.
			if !matched {
				if settings.unmatchedIsDelete() {
					d.CleanupAction = DELETE
					stats.ArtifactsForDeletion++
					stats.TotalSizeForDeletion += meta.Size
				} else {
					d.CleanupAction = UNMATCHED_KEEP
				}
			}
		}
	}

	return stats
}

// BuildDecisionMap groups artifact metadata by Group, sorts each group
// newest-first, and initialises all CleanupActions to UNDEFINED.
func BuildDecisionMap(artifacts []artifactory.Metadata) map[string][]CleanupDecision {
	dm := make(map[string][]CleanupDecision, len(artifacts))
	for _, a := range artifacts {
		dm[a.Group] = append(dm[a.Group], CleanupDecision{Artifact: a})
	}
	for group := range dm {
		sort.Slice(dm[group], func(i, j int) bool {
			return dm[group][i].Artifact.CreatedAt.After(*dm[group][j].Artifact.CreatedAt)
		})
	}
	return dm
}

// ComputeStats counts decisions by action across a grouped decision map.
func ComputeStats(decisions map[string][]CleanupDecision) CleanupStatistics {
	var stats CleanupStatistics
	for _, groupDecisions := range decisions {
		for _, d := range groupDecisions {
			stats.TotalArtifacts++
			stats.TotalSize += d.Artifact.Size
			switch d.CleanupAction {
			case DELETE:
				stats.ArtifactsForDeletion++
				stats.TotalSizeForDeletion += d.Artifact.Size
			case WHITELISTED:
				stats.ArtifactsWhitelisted++
			}
		}
	}
	return stats
}