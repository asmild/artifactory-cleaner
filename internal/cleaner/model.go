// Package cleaner contains the domain logic for retention decisions, plan
// execution, reporting, and configuration loading. It is strategy-agnostic —
// concrete cleanup strategies live in internal/strategy/*.
package cleaner

import "regexp"

import (
	"context"
	"time"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
)

// CleanupPlan holds the full state of a planned cleanup run.
type CleanupPlan struct {
	Repository         string
	DryRun             bool
	Stats              CleanupStatistics
	DeleteLimit        int
	GroupedDecisionMap map[string][]CleanupDecision
	Timestamp          time.Time
	artClient          Deleter
}

// Deleter is the subset of artifactory.Client used by Execute.
type Deleter interface {
	DeletePath(ctx context.Context, repo, path string) error
}

// CleanupStatistics summarises what was found and decided.
type CleanupStatistics struct {
	TotalArtifacts       int64
	ArtifactsForDeletion int64
	ArtifactsWhitelisted int64
	TotalSize            int64
	TotalSizeForDeletion int64
}

// CleanupDecision pairs an artifact with the action decided for it.
type CleanupDecision struct {
	CleanupAction CleanupAction
	RuleSettings  *RuleSettings
	Artifact      artifactory.Metadata
}

// CleanupAction describes what the cleaner will do with an artifact.
type CleanupAction int

const (
	UNDEFINED           CleanupAction = iota // artifact has not been evaluated yet
	RECENT_VERSION                           // kept: within the recentArtifactRetention count for its rule
	DOWNLOADED_RECENTLY                      // kept: downloaded within the lastDownloadedDays window
	WHITELISTED                              // kept: in the matched rule's whitelist
	MANIFEST_LIST_REF                        // kept: Docker platform image (sha256) referenced by a
	//                                          retained manifest list; deleting it would break docker pull
	PROTECTED        // kept: in target-level protectedVersions or protectedGroups — checked before rules
	CREATED_RECENTLY // kept: created within the rule's artifactLifetimeDays grace period; acts as a
	//                  safety net for newly built artifacts that haven't been downloaded yet
	UNMATCHED_KEEP  // kept: no rule pattern matched, unmatchedAction is "keep"
	KEEP_ORPHANED   // kept: no rule pattern matched, unmatchedAction is "keep"
	DELETE_ORPHANED // removed: manifest list with no underlying platform manifests
	DELETE          // removed: did not satisfy any keep condition
)

// CleanupActionStrings maps each action to its display label.
// Kept actions are prefixed with KEEP_ so the Action column is self-explanatory.
var CleanupActionStrings = map[CleanupAction]string{
	UNDEFINED:           "UNDEFINED",
	RECENT_VERSION:      "KEEP_RECENT_VERSION",
	DOWNLOADED_RECENTLY: "KEEP_DOWNLOADED_RECENTLY",
	CREATED_RECENTLY:    "KEEP_CREATED_RECENTLY",
	WHITELISTED:         "KEEP_WHITELISTED",
	PROTECTED:           "KEEP_PROTECTED",
	MANIFEST_LIST_REF:   "KEEP_MANIFEST_LIST_REF",
	UNMATCHED_KEEP:      "KEEP_UNMATCHED",
	DELETE:              "DELETE",
	DELETE_ORPHANED:     "DELETE_ORPHANED",
	KEEP_ORPHANED:       "KEEP_ORPHANED",
}

// TargetSettings is the per-repository configuration from the config file.
type TargetSettings struct {
	Name              string         `mapstructure:"name"`
	UnmatchedAction   string         `mapstructure:"unmatchedAction"`   // "keep" (default) | "delete"
	ProtectedVersions []string       `mapstructure:"protectedVersions"` // immune to all rules, checked first
	ProtectedGroups   []string       `mapstructure:"protectedGroups"`   // entire group immune to all rules
	Concurrency       int            `mapstructure:"concurrency"`       // parallel HTTP requests for manifest fetching (Docker); default 8
	Rules             []RuleSettings `mapstructure:"rules"`
}

// GetConcurrency returns the configured concurrency, or the default if unset.
func (s TargetSettings) GetConcurrency() int {
	if s.Concurrency > 0 {
		return s.Concurrency
	}
	return 8
}

// RuleSettings defines one retention rule within a target.
type RuleSettings struct {
	Name                        string   `mapstructure:"name"`
	Pattern                     string   `mapstructure:"pattern"`                 // regexp matched against version
	Discriminator               string   `mapstructure:"discriminator"`           // Maven/Generic: AQL filename filter
	PathMatcher                 string   `mapstructure:"pathMatcher"`             // Maven/Generic: AQL path filter
	RecentArtifactRetention     int      `mapstructure:"recentArtifactRetention"` // keep N most recent matches
	LastDownloadedDays          int      `mapstructure:"lastDownloadedDays"`      // keep if downloaded within N days
	ArtifactLifetimeDays        int      `mapstructure:"artifactLifetimeDays"`    // grace period: keep anything created within N days regardless of downloads
	WhitelistedGroups           []string `mapstructure:"whitelistedGroups"`       // rule-scoped: only when pattern matched
	WhitelistedVersions         []string `mapstructure:"whitelistedVersions"`
	WhitelistedArtifacts        []string `mapstructure:"whitelistedArtifacts"`        // "group@version" pairs
	DeleteOrphanedManifestsList bool     `mapstructure:"deleteOrphanedManifestsList"` // Docker: delete manifest lists with no underlying platform manifests
}

// MatchingRule returns the first rule whose pattern matches the given version, or nil if none match.
func (s TargetSettings) MatchingRule(version string) *RuleSettings {
	for i := range s.Rules {
		if matched, _ := regexp.MatchString(s.Rules[i].Pattern, version); matched {
			return &s.Rules[i]
		}
	}
	return nil
}

func (s TargetSettings) unmatchedIsDelete() bool {
	return s.UnmatchedAction == "delete"
}

// CleanupProperties is the top-level structure of the config file.
type CleanupProperties struct {
	Cleanup struct {
		Repositories map[string]TargetSettings `mapstructure:"repositories"`
	} `mapstructure:"cleanup"`
}

// Strategy is implemented by each package-type-specific cleanup strategy.
type Strategy interface {
	Plan(ctx context.Context, settings TargetSettings) (map[string][]CleanupDecision, error)
}
