package docker

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
	"github.com/asmild/artifactory-cleaner/internal/cleaner"
	"github.com/schollz/progressbar/v3"
)

// planMultiPlatform builds decisions for multi-platform Docker images.
//
// Decision flow (bottom-up):
//  1. AQL list.manifest.json → manifest list tags (created date only, stat ignored)
//  2. AQL manifest.json (non-sha256) → checksum index with stat.downloaded
//     These stats are NOT contaminated — the cleaner never reads manifest.json content.
//  3. Read each manifest list content → platform image digests (concurrent)
//  4. Build one virtual Metadata per manifest list version where
//     LastDownloadedAt = max(platform image stat.downloaded) — clean signal
//  5. MakeDecisions on virtual entries using manifest list tag for pattern matching
//  6. Propagate decision to all platform image paths (sha256 dirs + named arch tags)
//
// Returns the decision map and the set of paths already handled (for Pass 2 exclusion).
func planMultiPlatform(ctx context.Context, client artifactory.Repository, settings cleaner.TargetSettings) (map[string][]cleaner.CleanupDecision, map[string]bool, error) {
	type manifestResult struct {
		artifacts []artifactory.Metadata
		err       error
	}
	type checksumResult struct {
		index map[string][]artifactory.ManifestStat
		err   error
	}

	manifestCh := make(chan manifestResult, 1)
	checksumCh := make(chan checksumResult, 1)

	go func() {
		a, err := client.FindArtifacts(ctx, artifactory.ArtifactFilter{
			Repo: settings.Name,
			Name: "list.manifest.json",
		})
		manifestCh <- manifestResult{a, err}
	}()

	go func() {
		fmt.Println("Building platform image checksum index...")
		idx, err := client.FindManifestChecksums(ctx, settings.Name)
		checksumCh <- checksumResult{idx, err}
	}()

	mr := <-manifestCh
	if mr.err != nil {
		return nil, nil, fmt.Errorf("fetch manifest lists: %w", mr.err)
	}
	cr := <-checksumCh
	if cr.err != nil {
		return nil, nil, fmt.Errorf("build checksum index: %w", cr.err)
	}

	// Read manifest list content to get platform image digests.
	digestMap, err := readManifestListDigests(ctx, client, settings.Name, mr.artifacts, settings.ManifestConcurrency())
	if err != nil {
		return nil, nil, err
	}

	// Build one virtual artifact per manifest list version.
	// LastDownloadedAt comes from platform image stats — not contaminated by cleaner reads.
	virtualArtifacts := buildVirtualArtifacts(mr.artifacts, digestMap, cr.index)

	dm := cleaner.BuildDecisionMap(virtualArtifacts)
	cleaner.MakeDecisions(dm, settings)

	// Add synthetic decisions for all platform image paths.
	handledPaths := addPlatformImageDecisions(dm, digestMap, cr.index)

	// Fetch real sizes for sha256 platform image dirs concurrently,
	// then propagate the totals up to each manifest list tag entry.
	if err := fetchAndApplySizes(ctx, client, settings.Name, dm, digestMap, settings.ManifestConcurrency()); err != nil {
		return nil, nil, err
	}

	return dm, handledPaths, nil
}

// readManifestListDigests concurrently reads each manifest list and returns
// a map of artifactPath → []digest.
func readManifestListDigests(
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
	}

	total := len(manifestLists)
	fmt.Printf("Reading %d manifest lists to extract platform image digests:\n", total)
	bar := progressbar.New(total)

	jobCh := make(chan job, total)
	resCh := make(chan result, total)

	var warnMu sync.Mutex
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
					if err != nil {
						if ctx.Err() != nil {
							return
						}
						warnMu.Lock()
						fmt.Printf("\nwarning: could not read manifest for %s: %v\n", j.path, err)
						warnMu.Unlock()
					}
					resCh <- result{j.path, digests}
				}
			}
		}()
	}

	for _, ml := range manifestLists {
		jobCh <- job{ml.Path}
	}
	close(jobCh)

	go func() { wg.Wait(); close(resCh) }()

	digestMap := make(map[string][]string, total)
	for res := range resCh {
		digestMap[res.path] = res.digests
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fmt.Printf("\n\n")
	return digestMap, nil
}

// buildVirtualArtifacts creates one Metadata entry per manifest list, where
// LastDownloadedAt is the most recent stat.downloaded across all its platform
// images — a clean signal unaffected by cleaner reads of list.manifest.json.
func buildVirtualArtifacts(
	manifestLists []artifactory.Metadata,
	digestMap map[string][]string,
	checksumIndex map[string][]artifactory.ManifestStat,
) []artifactory.Metadata {
	result := make([]artifactory.Metadata, 0, len(manifestLists))

	for _, ml := range manifestLists {
		var effectiveDownload *time.Time

		for _, digest := range digestMap[ml.Path] {
			shortDigest := strings.TrimPrefix(digest, "sha256:")
			for _, stat := range checksumIndex[shortDigest] {
				if stat.DownloadedAt != nil {
					if effectiveDownload == nil || stat.DownloadedAt.After(*effectiveDownload) {
						t := *stat.DownloadedAt
						effectiveDownload = &t
					}
				}
			}
		}

		result = append(result, artifactory.Metadata{
			Path:             ml.Path,
			Group:            ml.Group,
			Version:          ml.Version, // manifest list tag used for pattern matching
			ManifestListTag:  ml.Version, // manifest lists are their own tag
			Size:             ml.Size,
			CreatedAt:        ml.CreatedAt,
			LastDownloadedAt: effectiveDownload, // from platform images ✓ clean
		})
	}
	return result
}

// addPlatformImageDecisions adds synthetic CleanupDecision entries for every
// platform image directory and named arch tag belonging to each manifest list.
// Platform images that belong to a KEPT manifest list get MANIFEST_LIST_REF;
// those belonging to a DELETED list get DELETE.
// Returns the set of artifact paths handled here so Pass 2 can skip them.
// fetchAndApplySizes concurrently fetches the real size of every sha256 platform
// image directory via the Artifactory artifactsCount API, updates those entries
// in the decision map, and sets each manifest list tag's size to the sum of its
// platform images so the report shows the actual Docker image footprint.
func fetchAndApplySizes(
	ctx context.Context,
	client artifactory.Repository,
	repo string,
	decisions map[string][]cleaner.CleanupDecision,
	digestMap map[string][]string,
	concurrency int,
) error {
	// Collect all unique sha256 dir paths we need sizes for.
	type job struct{ path string }
	type result struct {
		path string
		size int64
	}

	seen := make(map[string]bool)
	var jobs []job
	for _, digests := range digestMap {
		for _, digest := range digests {
			// sha256 dir path = group + "/" + digest
			for group := range decisions {
				p := group + "/" + digest
				if !seen[p] {
					seen[p] = true
					jobs = append(jobs, job{p})
				}
			}
		}
	}
	// Simpler: collect from decisions directly.
	seen = make(map[string]bool)
	jobs = nil
	for _, groupDecisions := range decisions {
		for _, d := range groupDecisions {
			if strings.HasPrefix(d.Artifact.Version, "sha256:") && !seen[d.Artifact.Path] {
				seen[d.Artifact.Path] = true
				jobs = append(jobs, job{d.Artifact.Path})
			}
		}
	}

	if len(jobs) == 0 {
		return nil
	}

	fmt.Printf("Fetching sizes for %d platform image directories:\n", len(jobs))
	bar := progressbar.New(len(jobs))

	jobCh := make(chan job, len(jobs))
	resCh := make(chan result, len(jobs))

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
					size, _ := client.FetchDirectorySize(ctx, repo, j.path)
					bar.Add(1) //nolint:errcheck
					resCh <- result{j.path, size}
				}
			}
		}()
	}
	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)
	go func() { wg.Wait(); close(resCh) }()

	// Build path → size map.
	sizeByPath := make(map[string]int64, len(jobs))
	for r := range resCh {
		sizeByPath[r.path] = r.size
	}
	fmt.Printf("\n\n")

	if err := ctx.Err(); err != nil {
		return err
	}

	// Update sha256 dir decisions with real sizes.
	for group, groupDecisions := range decisions {
		for i := range groupDecisions {
			d := &groupDecisions[i]
			if strings.HasPrefix(d.Artifact.Version, "sha256:") {
				d.Artifact.Size = sizeByPath[d.Artifact.Path]
			}
		}

		// Set manifest list tag size = sum of its platform image sizes.
		for i := range groupDecisions {
			d := &groupDecisions[i]
			if !strings.HasPrefix(d.Artifact.Version, "sha256:") && d.Artifact.ManifestListTag == d.Artifact.Version {
				// This is a manifest list entry — sum its platform dirs.
				var total int64
				for _, digest := range digestMap[group+"/"+d.Artifact.Version] {
					total += sizeByPath[group+"/"+digest]
				}
				if total > 0 {
					d.Artifact.Size = total
				}
			}
		}
	}
	return nil
}

func addPlatformImageDecisions(
	decisions map[string][]cleaner.CleanupDecision,
	digestMap map[string][]string,
	checksumIndex map[string][]artifactory.ManifestStat,
) map[string]bool {
	handledPaths := make(map[string]bool)

	// Build path → action lookup from current decisions.
	pathAction := make(map[string]cleaner.CleanupAction)
	for _, groupDecisions := range decisions {
		for _, d := range groupDecisions {
			pathAction[d.Artifact.Path] = d.CleanupAction
		}
	}

	for mlPath, digests := range digestMap {
		action, ok := pathAction[mlPath]
		if !ok {
			continue
		}
		group := splitGroup(mlPath)
		mlTag := splitVersion(mlPath) // e.g. "2.26.0"

		// Platform images inherit MANIFEST_LIST_REF when kept, DELETE when deleted.
		platformAction := cleaner.MANIFEST_LIST_REF
		if action == cleaner.DELETE {
			platformAction = cleaner.DELETE
		}

		for _, digest := range digests {
			// 1. sha256-addressed directory (content-addressed, any format).
			sha256DirPath := group + "/" + digest
			if !handledPaths[sha256DirPath] {
				handledPaths[sha256DirPath] = true
				decisions[group] = append(decisions[group], cleaner.CleanupDecision{
					CleanupAction: platformAction,
					Artifact: artifactory.Metadata{
						Path:            sha256DirPath,
						Group:           group,
						Version:         digest,
						ManifestListTag: mlTag,
						// Size set later by fetchAndApplySizes
					},
				})
			}

			// 2. Named arch tags sharing the same content (amd64-1.5.1, arm64-1.5.1, etc.)
			//    identified via checksum reverse index.
			shortDigest := strings.TrimPrefix(digest, "sha256:")
			for _, stat := range checksumIndex[shortDigest] {
				if splitGroup(stat.Path) != group || handledPaths[stat.Path] {
					continue
				}
				handledPaths[stat.Path] = true
				decisions[group] = append(decisions[group], cleaner.CleanupDecision{
					CleanupAction: platformAction,
					Artifact: artifactory.Metadata{
						Path:             stat.Path,
						Group:            group,
						Version:          splitVersion(stat.Path),
						ManifestListTag:  mlTag,
						LastDownloadedAt: stat.DownloadedAt,
					},
				})
			}
		}
	}

	return handledPaths
}

func splitGroup(artifactPath string) string {
	i := strings.LastIndex(artifactPath, "/")
	if i <= 0 {
		return ""
	}
	return artifactPath[:i]
}

func splitVersion(artifactPath string) string {
	i := strings.LastIndex(artifactPath, "/")
	if i < 0 {
		return artifactPath
	}
	return artifactPath[i+1:]
}
