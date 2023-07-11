package cleaner

import (
	"github.com/asmild/artifactory-cleaner/aql"
	"github.com/asmild/artifactory-cleaner/util"
	"regexp"
	"sort"
	"sync/atomic"
	"time"
)

// contains reports whether v is present in s.
func makeDecisions(cleanupDecisions *map[string][]CleanupDecision, cleanupRepositoriesProperties RepositoryCleanupSettings) CleanupStatistics {
	recentlyUsedThreshold := time.Now().AddDate(0, 0, -cleanupRepositoriesProperties.LastDownloadedDays)
	var totalArtifacts int64
	var artifactsForDeletion int64
	var artifactsWhitelisted int64
	var totalSize int64
	var totalSizeForDeletion int64

	for group, groupCleanupItems := range *cleanupDecisions {
		// Check if artifact group is whitelisted
		groupIsWhitelisted := false

		// TODO: whitelisted regex ?
		if util.Contains(cleanupRepositoriesProperties.WhitelistedGroups, group) {
			groupIsWhitelisted = true
		}

		for i := range groupCleanupItems {
			decision := &groupCleanupItems[i]
			lastUsage := decision.ArtifactMetadata.LastDownloadedAt
			artifactVersion := decision.ArtifactMetadata.Version
			artifactSize := decision.ArtifactMetadata.Size
			path := decision.ArtifactMetadata.Path

			atomic.AddInt64(&totalArtifacts, 1)
			atomic.AddInt64(&totalSize, artifactSize)

			//	Keep whitelisted artifact group
			if groupIsWhitelisted {
				decision.CleanupAction = WHITELISTED
				atomic.AddInt64(&artifactsWhitelisted, 1)
			}

			// Keep whitelisted versions
			// TODO: think of whitelist by regex
			if util.Contains(cleanupRepositoriesProperties.WhitelistedVersions, artifactVersion) {
				atomic.AddInt64(&artifactsWhitelisted, 1)
				decision.CleanupAction = WHITELISTED
			}

			// Keep whitelisted artifacts
			if util.Contains(cleanupRepositoriesProperties.WhitelistedArtifacts, group+"@"+artifactVersion) {
				atomic.AddInt64(&artifactsWhitelisted, 1)
				decision.CleanupAction = WHITELISTED
			}

			// Keep recent released versions
			if decision.CleanupAction == UNDEFINED {
				if matched, _ := regexp.MatchString(cleanupRepositoriesProperties.CleanVersionPattern, artifactVersion); matched {
					if i < cleanupRepositoriesProperties.RecentArtifactRetention {
						decision.CleanupAction = RECENT_VERSION
					}
				}
			}

			// Keep versions that was downloaded recently
			if decision.CleanupAction == UNDEFINED {
				if lastUsage == nil {
					lastUsage = decision.ArtifactMetadata.CreatedAt
				}

				if lastUsage.After(recentlyUsedThreshold) {
					decision.CleanupAction = DOWNLOADED_RECENTLY
				}
			}

			// Mark others to delete
			if decision.CleanupAction == UNDEFINED {
				decision.CleanupAction = DELETE
				atomic.AddInt64(&artifactsForDeletion, 1)
				atomic.AddInt64(&totalSizeForDeletion, artifactSize)
			}
		}
	}

	return CleanupStatistics{
		TotalArtifacts:       totalArtifacts,
		ArtifactsForDeletion: artifactsForDeletion,
		ArtifactsWhitelisted: artifactsWhitelisted,
		TotalSize:            totalSize,
		TotalSizeForDeletion: totalSizeForDeletion,
	}
}

func createUndecidedGroupedDecisionMap(cleanupRepositoriesProperties RepositoryCleanupSettings) (map[string][]CleanupDecision, error) {
	a, err := aql.New()
	var artifacts []aql.ArtifactMetadata
	decisionMap := make(map[string][]CleanupDecision)
	if err != nil {
		return decisionMap, err
	}

	artifacts, err = a.GetArtifacts(
		cleanupRepositoriesProperties.Name,
		cleanupRepositoriesProperties.PathMatcher,
		cleanupRepositoriesProperties.Discriminator)
	if err != nil {
		return decisionMap, err
	}

	for _, artifact := range artifacts {
		decision := CleanupDecision{
			ArtifactMetadata: artifact,
		}

		decisionMap[artifact.Group] = append(decisionMap[artifact.Group], decision)
	}

	// Sort the decisions within each group by CreatedAt in descending order
	for _, decisions := range decisionMap {
		sort.Slice(decisions, func(i, j int) bool {
			return decisions[i].ArtifactMetadata.CreatedAt.After(*decisions[j].ArtifactMetadata.CreatedAt)
		})
	}
	return decisionMap, nil
}
