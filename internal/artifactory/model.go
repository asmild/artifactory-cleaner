// Package artifactory wraps the JFrog Artifactory REST and AQL APIs.
package artifactory

import "time"

// Item is a raw result row from an AQL query.
type Item struct {
	Repo    string    `json:"repo"`
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	SHA256  string    `json:"sha256"`
	Created time.Time `json:"created"`
	Stats   []struct {
		Downloaded time.Time `json:"downloaded"`
	} `json:"stats,omitempty"`
}

type aqlResults struct {
	Results []Item `json:"results"`
}

// RepoInfo holds metadata about an Artifactory repository.
type RepoInfo struct {
	Key         string `json:"key"`
	RClass      string `json:"rclass"`      // "local", "remote", or "virtual"
	PackageType string `json:"packageType"` // "Docker", "Maven", "Generic", etc.
}

// Metadata is the processed, domain-level view of an artifact used by strategies
// and the cleaner. It is derived from AQL Item rows.
type Metadata struct {
	Path             string
	Group            string   // parent path segment (image name, Maven groupId+artifactId)
	Version          string   // last path segment (tag, version number)
	Parent           []string // for Docker platform images: the manifest list tag they belong to (e.g. "2.26.0")
	SHA256           string
	Size             int64
	CreatedAt        *time.Time
	LastDownloadedAt *time.Time
}

// manifestList holds the fields of a Docker/OCI manifest list file (list.manifest.json).
type manifestList struct {
	Manifests []struct {
		Digest string `json:"digest"`
	} `json:"manifests"`
}

// ArtifactFilter defines the criteria for an AQL artifact search.
type ArtifactFilter struct {
	Repo        string
	Name        string // exact filename or glob, e.g. "list.manifest.json" or "*.pom"
	PathMatch   string // glob for $match on path (optional)
	PathNoMatch string // glob for $nmatch on path (optional)
}
