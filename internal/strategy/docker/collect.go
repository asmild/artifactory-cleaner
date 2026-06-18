package docker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
	"github.com/asmild/artifactory-cleaner/internal/cleaner"
	"github.com/schollz/progressbar/v3"
)

// fetchMetadata fetches manifest lists and all manifest.json files in parallel.
//
// Returns:
//   - manifestLists: all list.manifest.json entries (one per manifest list tag)
//   - manifestIndex: sha256 → []Metadata for all manifest.json in the repo
func fetchMetadata(ctx context.Context, client artifactory.Repository, repo string) (
	[]artifactory.Metadata, map[string][]*artifactory.Metadata, error,
) {
	manifestLists, err := client.FindArtifacts(ctx, artifactory.ArtifactFilter{
		Repo: repo,
		Name: "list.manifest.json",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("fetch manifest lists: %w", err)
	}

	manifests, err := client.FindArtifacts(ctx, artifactory.ArtifactFilter{
		Repo: repo,
		Name: "manifest.json",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("fetch manifests: %w", err)
	}

	manifestIndex := make(map[string][]*artifactory.Metadata, len(manifests))
	for i := range manifests {
		m := &manifests[i]
		if m.SHA256 != "" {
			manifestIndex[m.SHA256] = append(manifestIndex[m.SHA256], m)
		}
	}

	return manifestLists, manifestIndex, nil
}

// computeEffectiveDownload reads platform manifests from manifestIndex for the given
// digests and returns how many were found plus the most recent download time across all of them.
func computeEffectiveDownload(digests []string, manifestIndex map[string][]*artifactory.Metadata) (foundCount int, effectiveDownload *time.Time) {
	for _, digest := range digests {
		manifests, ok := manifestIndex[digest]
		if !ok {
			continue
		}
		foundCount++
		for _, m := range manifests {
			if m.LastDownloadedAt != nil {
				if effectiveDownload == nil || m.LastDownloadedAt.After(*effectiveDownload) {
					effectiveDownload = m.LastDownloadedAt
				}
			}
		}
	}
	return foundCount, effectiveDownload
}

// linkPlatformDecisions pops each digest from manifestIndex and creates a pending
// platform decision for every manifest path found, linking it to the parent manifest list.
func linkPlatformDecisions(digests []string, manifestIndex map[string][]*artifactory.Metadata, mld *cleaner.CleanupDecision, decisions map[string]*cleaner.CleanupDecision) {
	for _, digest := range digests {
		manifests, ok := manifestIndex[digest]
		if !ok {
			continue
		}

		for _, m := range manifests {
			if existing, ok := decisions[m.Path]; ok {
				// Platform already linked to another manifest list — append this parent.
				existing.Artifact.Parent = append(existing.Artifact.Parent, mld.Artifact.Path)
				continue
			}
			m.Parent = []string{mld.Artifact.Path}
			md := &cleaner.CleanupDecision{Artifact: *m, CleanupAction: pending}
			decisions[m.Path] = md
		}
	}
}

// fetchManifestListDigests concurrently reads platform digests for all manifest lists
// via the Docker v2 API and returns manifestList.Path → []platformDigest.
// The v2 API does NOT update stat.downloaded on list.manifest.json files.
func fetchManifestListDigests(
	ctx context.Context,
	client artifactory.Repository,
	repo string,
	manifestLists []artifactory.Metadata,
	concurrency int,
) (map[string][]string, error) {
	type job struct{ path string }
	type result struct {
		path    string
		digests []string
		err     error
	}

	total := len(manifestLists)
	bar := progressbar.New(total)

	jobCh := make(chan job, total)
	resCh := make(chan result, total)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case j, ok := <-jobCh:
					if !ok {
						return
					}
					digests, err := client.GetManifestListDigests(ctx, repo, j.path)
					bar.Add(1) //nolint:errcheck
					resCh <- result{j.path, digests, err}
				}
			}
		}()
	}

	for _, ml := range manifestLists {
		jobCh <- job{ml.Path}
	}
	close(jobCh)
	go func() { wg.Wait(); close(resCh) }()

	digestsByPath := make(map[string][]string, total)
	for r := range resCh {
		if r.err != nil && ctx.Err() == nil {
			fmt.Printf("\nwarning: could not read manifest list %s: %v\n", r.path, r.err)
		}
		digestsByPath[r.path] = r.digests
	}
	fmt.Printf("\n\n")

	return digestsByPath, ctx.Err()
}
