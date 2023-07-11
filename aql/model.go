package aql
import (
	"time"
)

type ArtifactView struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Size    int64       `json:"size"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	Stats   []struct {
		Downloaded time.Time `json:"downloaded"`
	} `json:"stats,omitempty"`
}

type Results struct {
	Artifacts []ArtifactView `json:"results"`
	Range   struct {
		StartPos int `json:"start_pos"`
		EndPos   int `json:"end_pos"`
		Total    int `json:"total"`
	} `json:"range"`
}

type ArtifactMetadata struct {
	Path             string
	Group            string
	Version          string
	Size             int64
	CreatedAt        *time.Time
	LastUpdatedAt    *time.Time
	LastDownloadedAt *time.Time
}

func (am ArtifactMetadata) GetLastDownloadedAt() *time.Time {
	return am.LastDownloadedAt
}
