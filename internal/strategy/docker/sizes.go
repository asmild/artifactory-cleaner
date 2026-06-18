package docker

import (
	"context"
	"fmt"
	"sync"

	"github.com/asmild/artifactory-cleaner/internal/cleaner"
	"github.com/schollz/progressbar/v3"
)

// fetchAndApplySizes fetches the real directory size for every DELETE decision
// and sets it on the artifact. Runs concurrently up to concurrency workers.
func fetchAndApplySizes(
	ctx context.Context,
	client interface {
		FetchDirectorySize(ctx context.Context, repo, artifactPath string) (int64, error)
	},
	repo string,
	decisions map[string]*cleaner.CleanupDecision,
	concurrency int,
) error {
	type job struct {
		path string
		d    *cleaner.CleanupDecision
	}

	var jobs []job
	for _, d := range decisions {
		if d.CleanupAction == cleaner.DELETE || d.CleanupAction == cleaner.DELETE_ORPHANED {
			jobs = append(jobs, job{d.Artifact.Path, d})
		}
	}
	if len(jobs) == 0 {
		return nil
	}

	fmt.Printf("Fetching sizes for %d artifacts to delete:\n", len(jobs))
	bar := progressbar.New(len(jobs))

	jobCh := make(chan job, len(jobs))
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
					j.d.Artifact.Size = size
					bar.Add(1) //nolint:errcheck
				}
			}
		}()
	}

	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)
	wg.Wait()
	fmt.Printf("\n\n")

	return ctx.Err()
}
