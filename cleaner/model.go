package cleaner

import (
	"github.com/asmild/artifactory-cleaner/aql"
	"time"
)

type CleanupPlan struct {
	Repository         string
	DryRun             bool
	Stats              CleanupStatistics
	GroupedDecisionMap map[string][]CleanupDecision
	Timestamp          time.Time
	AQL                *aql.AQL
}

type CleanupReporter struct {
}

type CleanupStatistics struct {
	TotalArtifacts       int64
	ArtifactsForDeletion int64
	ArtifactsWhitelisted int64
	TotalSize            int64
	TotalSizeForDeletion int64
}

type CleanupDecision struct {
	CleanupAction    CleanupAction
	ArtifactMetadata aql.ArtifactMetadata
}

type CleanupAction int

const (
	UNDEFINED           CleanupAction = iota // 0
	RECENT_VERSION                           // 1
	DOWNLOADED_RECENTLY                      // 2
	WHITELISTED                              // 3
	DEPLOYED                                 // 4
	DELETE                                   // 5
)

var CleanupActionStrings = map[CleanupAction]string{
	UNDEFINED:           "UNDEFINED",
	RECENT_VERSION:      "RECENT_VERSION",
	DOWNLOADED_RECENTLY: "DOWNLOADED_RECENTLY",
	WHITELISTED:         "WHITELISTED",
	DEPLOYED:            "DEPLOYED",
	DELETE:              "DELETE",
}

type RepositoryCleanupSettings struct {
	Name                    string   `mapstructure:"name"`
	CleanVersionPattern     string   `mapstructure:"cleanVersionPattern"`
	PathMatcher             string   `mapstructure:"pathMatcher"`
	Discriminator           string   `mapstructure:"discriminator"`
	RecentArtifactRetention int      `mapstructure:"recentArtifactRetention"`
	LastDownloadedDays      int      `mapstructure:"lastDownloadedDays"`
	WhitelistedGroups       []string `mapstructure:"whitelistedGroups"`
	WhitelistedVersions     []string `mapstructure:"whitelistedVersions"`
	WhitelistedArtifacts    []string `mapstructure:"whitelistedArtifacts"`
}

type CleanupProperties struct {
	Cleanup struct {
		Repositories map[string]RepositoryCleanupSettings `mapstructure:"repositories"`
	} `mapstructure:"cleanup"`
}
