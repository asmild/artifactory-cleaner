package docker

import (
	"context"
	"fmt"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
	"github.com/asmild/artifactory-cleaner/internal/cleaner"
)

// planStandalone builds decisions for single-platform Docker images:
// manifest.json files whose paths do not contain "sha256:" and that were
// not already handled by planMultiPlatform (i.e. not identified as a platform
// image belonging to a manifest list via checksum matching).
func planStandalone(ctx context.Context, client artifactory.Repository, settings cleaner.TargetSettings, handledPaths map[string]bool) (map[string][]cleaner.CleanupDecision, error) {
	artifacts, err := client.FindArtifacts(ctx, artifactory.ArtifactFilter{
		Repo:        settings.Name,
		Name:        "manifest.json",
		PathNoMatch: "*/sha256:*",
	})
	if err != nil {
		return nil, fmt.Errorf("fetch standalone manifests: %w", err)
	}

	// Exclude paths already assigned a decision in Pass 1.
	// These are named platform-image tags (e.g. amd64-1.5.1) identified as
	// belonging to a manifest list via content-hash matching.
	standalone := artifacts[:0]
	for _, a := range artifacts {
		if !handledPaths[a.Path] {
			standalone = append(standalone, a)
		}
	}

	dm := cleaner.BuildDecisionMap(standalone)
	cleaner.MakeDecisions(dm, settings)
	return dm, nil
}
