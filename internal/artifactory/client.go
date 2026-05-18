package artifactory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	http "github.com/asmild/artifactory-cleaner/internal/client"
	"github.com/schollz/progressbar/v3"
)

// Repository is the interface strategies use to interact with Artifactory.
// *Client satisfies it; define it here so strategies can mock it without
// importing each other.
type Repository interface {
	FindArtifacts(ctx context.Context, f ArtifactFilter) ([]Metadata, error)
	FindManifestChecksums(ctx context.Context, repo string) (map[string][]ManifestStat, error)
	GetManifestListDigests(ctx context.Context, repo, artifactPath string) ([]string, error)
	DeletePath(ctx context.Context, repo, artifactPath string) error
}

// Client wraps the HTTP client with Artifactory AQL and REST operations.
type Client struct {
	http *http.Client
}

// New creates a Client from environment-configured credentials.
func New() (*Client, error) {
	cfg, err := http.NewConfig()
	if err != nil {
		return nil, err
	}
	c, err := http.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{http: c}, nil
}

// GetRepoInfo fetches metadata for the given repository key.
func (c *Client) GetRepoInfo(ctx context.Context, repoKey string) (RepoInfo, error) {
	data, err := c.http.Get(ctx, fmt.Sprintf("/artifactory/api/repositories/%s", repoKey))
	if err != nil {
		return RepoInfo{}, fmt.Errorf("get repo info for %q: %w", repoKey, err)
	}
	var info RepoInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return RepoInfo{}, fmt.Errorf("parse repo info: %w", err)
	}
	return info, nil
}

// FindArtifacts executes an AQL search and returns the matching items with metadata.
func (c *Client) FindArtifacts(ctx context.Context, f ArtifactFilter) ([]Metadata, error) {
	fmt.Printf("Fetching artifacts from %q (name=%s)... ", f.Repo, f.Name)

	items, err := c.queryArtifacts(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("fetch artifacts: %w", err)
	}

	fmt.Printf("found %d. Getting metadata:\n", len(items))

	bar := progressbar.New(len(items))
	results := make([]Metadata, 0, len(items))
	for _, item := range items {
		bar.Add(1) //nolint:errcheck
		results = append(results, toMetadata(item))
	}
	fmt.Printf("\n\n")
	return results, nil
}

// FindManifestChecksums returns a reverse index: sha256 → []ManifestStat for all
// manifest.json files in the repo, excluding sha256-tagged paths.
// Including stat.downloaded allows callers to derive the effective last-pull time
// for a manifest list from its platform images — whose stats are never contaminated
// by the cleaner (the cleaner only reads list.manifest.json, not manifest.json).
func (c *Client) FindManifestChecksums(ctx context.Context, repo string) (map[string][]ManifestStat, error) {
	const query = `items.find({"repo":%q,"name":"manifest.json","path":{"$nmatch":"*/sha256:*"}}).include("path","sha256","stat.downloaded")`

	data, err := c.http.Post(ctx, "/artifactory/api/search/aql", fmt.Sprintf(query, repo))
	if err != nil {
		return nil, fmt.Errorf("fetch manifest checksums: %w", err)
	}

	var result struct {
		Results []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
			Stats  []struct {
				Downloaded time.Time `json:"downloaded"`
			} `json:"stats,omitempty"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse manifest checksums: %w", err)
	}

	index := make(map[string][]ManifestStat, len(result.Results))
	for _, r := range result.Results {
		if r.SHA256 == "" {
			continue
		}
		var downloadedAt *time.Time
		if len(r.Stats) > 0 {
			t := r.Stats[0].Downloaded
			downloadedAt = &t
		}
		index[r.SHA256] = append(index[r.SHA256], ManifestStat{
			Path:         r.Path,
			DownloadedAt: downloadedAt,
		})
	}
	return index, nil
}

// GetOptionalFile fetches file content at repo/filePath.
// Returns (nil, nil) when the file does not exist (HTTP 404).
func (c *Client) GetOptionalFile(ctx context.Context, repo, filePath string) ([]byte, error) {
	return c.http.GetOptional(ctx, fmt.Sprintf("/artifactory/%s/%s", repo, filePath))
}

// GetManifestListDigests fetches the manifest list digests for artifactPath via the
// Docker Registry v2 API. Does NOT update stat.downloaded on list.manifest.json.
func (c *Client) GetManifestListDigests(ctx context.Context, repo, artifactPath string) ([]string, error) {
	image, tag := splitPath(artifactPath)
	url := fmt.Sprintf("/artifactory/api/docker/%s/v2/%s/manifests/%s", repo, image, tag)
	data, err := c.http.GetWithHeaders(ctx, url, map[string]string{
		"Accept": "application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.oci.image.index.v1+json",
	})
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}

	var m manifestList
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest list at %s: %w", artifactPath, err)
	}

	if len(m.Manifests) == 0 {
		return nil, nil
	}

	digests := make([]string, 0, len(m.Manifests))
	for _, entry := range m.Manifests {
		digests = append(digests, entry.Digest)
	}
	return digests, nil
}

// DeletePath deletes the artifact or directory at repo/artifactPath recursively.
func (c *Client) DeletePath(ctx context.Context, repo, artifactPath string) error {
	return c.http.Delete(ctx, fmt.Sprintf("/artifactory/%s/%s", repo, artifactPath))
}

// ── internal ─────────────────────────────────────────────────────────────────

func (c *Client) queryArtifacts(ctx context.Context, f ArtifactFilter) ([]Item, error) {
	query := buildAQL(f)
	const includeFields = `.include("repo","path","name","size","created","stat.downloaded")`

	data, err := c.http.Post(ctx, "/artifactory/api/search/aql", query+includeFields)
	if err != nil {
		return nil, err
	}

	var result aqlResults
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal AQL response: %w", err)
	}
	return result.Results, nil
}

func buildAQL(f ArtifactFilter) string {
	var conditions []string
	conditions = append(conditions, fmt.Sprintf(`"repo":%q`, f.Repo))
	if f.Name != "" {
		if strings.ContainsAny(f.Name, "*?") {
			conditions = append(conditions, fmt.Sprintf(`"name":{"$match":%q}`, f.Name))
		} else {
			conditions = append(conditions, fmt.Sprintf(`"name":%q`, f.Name))
		}
	}
	if f.PathMatch != "" {
		conditions = append(conditions, fmt.Sprintf(`"path":{"$match":%q}`, f.PathMatch))
	}
	if f.PathNoMatch != "" {
		conditions = append(conditions, fmt.Sprintf(`"path":{"$nmatch":%q}`, f.PathNoMatch))
	}
	return fmt.Sprintf("items.find({%s})", strings.Join(conditions, ","))
}

func toMetadata(item Item) Metadata {
	group, version := splitPath(item.Path)

	var lastDownloadedAt *time.Time
	if len(item.Stats) > 0 {
		t := item.Stats[0].Downloaded
		lastDownloadedAt = &t
	}

	return Metadata{
		Path:             item.Path,
		Group:            group,
		Version:          version,
		Size:             item.Size,
		CreatedAt:        &item.Created,
		LastDownloadedAt: lastDownloadedAt,
	}
}

func splitPath(path string) (group, version string) {
	i := strings.LastIndex(path, "/")
	if i <= 0 {
		return "", path
	}
	return path[:i], path[i+1:]
}
