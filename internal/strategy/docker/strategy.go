// Package docker implements the Docker cleanup strategy.
package docker

import (
	"context"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
	"github.com/asmild/artifactory-cleaner/internal/cleaner"
)

// pending is an internal state used during the cleanup decision process to indicate that a manifest list or standalone manifest is still pending evaluation.
// It is never visible in the final report.
const pending cleaner.CleanupAction = -1

// Strategy implements cleaner.Strategy for Docker repositories.
type Strategy struct {
	client artifactory.Repository
}

// New returns a DockerStrategy backed by the given client.
func New(client artifactory.Repository) *Strategy {
	return &Strategy{client: client}
}

// Plan runs the full Docker cleanup strategy and returns a grouped decision map.
func (s *Strategy) Plan(ctx context.Context, settings cleaner.TargetSettings) (map[string][]cleaner.CleanupDecision, error) {
	repo := settings.Name
	concurrency := settings.GetConcurrency()

	manifestLists, manifestIndex, err := fetchMetadata(ctx, s.client, repo)
	if err != nil {
		return nil, err
	}

	decisions := make(map[string]*cleaner.CleanupDecision)

	digestsByPath, err := fetchManifestListDigests(ctx, s.client, repo, manifestLists, concurrency)
	if err != nil {
		return nil, err
	}

	// Step 1 - iterate over manifest lists
	applyManifestListDecisions(manifestLists, digestsByPath, manifestIndex, decisions, &settings)

	// Step 2 - iterate over all standalone manifests and make decisions for them.
	applyStandaloneDecisions(manifestIndex, decisions, &settings)

	// Step 3 - resolve retention counts for all pending entries.
	// Both manifest lists and standalones share the same counter per (group, ruleName)
	// so versions aren't double-counted across types for the same image.
	resolveRetentionCounts(decisions)

	// Propagate final action from manifest list to its platform manifests.
	propagatePlatformDecisions(decisions)

	if err = fetchAndApplySizes(ctx, s.client, repo, decisions, concurrency); err != nil {
		return nil, err
	}

	result := make(map[string][]cleaner.CleanupDecision)
	for _, d := range decisions {
		result[d.Artifact.Group] = append(result[d.Artifact.Group], *d)
	}
	return result, nil
}

//1.5.2-master-SNAPSHOT
