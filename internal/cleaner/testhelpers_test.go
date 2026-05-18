package cleaner

import (
	"context"
	"time"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
)

func ptr[T any](v T) *T { return &v }

var (
	now = time.Now()
	old = now.AddDate(-2, 0, 0)
)

func decision(group, version string, created time.Time) CleanupDecision {
	return CleanupDecision{
		Artifact: artifactory.Metadata{
			Path:      group + "/" + version,
			Group:     group,
			Version:   version,
			Size:      1024,
			CreatedAt: ptr(created),
		},
	}
}

func decisionDownloaded(group, version string, created, downloaded time.Time) CleanupDecision {
	d := decision(group, version, created)
	d.Artifact.LastDownloadedAt = ptr(downloaded)
	return d
}

// singleRuleSettings returns a TargetSettings with one rule — mirrors the old
// flat RepositorySettings for tests that only care about one pattern.
func singleRuleSettings(pattern string, retention int, downloadedDays int) TargetSettings {
	return TargetSettings{
		UnmatchedAction: "keep",
		Rules: []RuleSettings{
			{
				Name:                    "default",
				Pattern:                 pattern,
				RecentArtifactRetention: retention,
				LastDownloadedDays:      downloadedDays,
			},
		},
	}
}

func defaultSettings() TargetSettings {
	return singleRuleSettings(`\d+\.\d+\.\d+`, 3, 90)
}

// mockDeleter records deleted paths and can return an error.
type mockDeleter struct {
	deleted   []string
	deleteErr error
}

func (m *mockDeleter) DeletePath(_ context.Context, _, path string) error {
	m.deleted = append(m.deleted, path)
	return m.deleteErr
}