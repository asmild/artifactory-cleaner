// Package docker implements the Docker cleanup strategy.
//
// Two passes are run for each repository:
//
//  1. Multi-platform pass (list.manifest.json files):
//     Retention decisions are driven by platform image pull statistics — the
//     stat.downloaded of each manifest.json is used, not the manifest list's
//     own stat, which is contaminated by the cleaner reading its content.
//     See docs/docker-cleanup-flow.md for the full design rationale.
//
//  2. Standalone pass (manifest.json files, non-sha256 paths):
//     Single-platform images not referenced by any manifest list, evaluated
//     independently using their own retention rules and pull statistics.
package docker

import (
	"context"
	"fmt"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
	"github.com/asmild/artifactory-cleaner/internal/cleaner"
)

// Strategy implements cleaner.Strategy for Docker repositories.
type Strategy struct {
	client artifactory.Repository
}

// New returns a DockerStrategy backed by the given client.
func New(client artifactory.Repository) *Strategy {
	return &Strategy{client: client}
}

// Plan runs both passes and merges the results into a single grouped decision map.
func (s *Strategy) Plan(ctx context.Context, settings cleaner.TargetSettings) (map[string][]cleaner.CleanupDecision, error) {
	fmt.Println("Pass 1 — multi-platform images (list.manifest.json)...")
	multiPlatform, handledPaths, err := planMultiPlatform(ctx, s.client, settings)
	if err != nil {
		return nil, fmt.Errorf("docker multi-platform pass: %w", err)
	}

	fmt.Println("Pass 2 — single-platform images (manifest.json)...")
	standalone, err := planStandalone(ctx, s.client, settings, handledPaths)
	if err != nil {
		return nil, fmt.Errorf("docker standalone pass: %w", err)
	}

	return merge(multiPlatform, standalone), nil
}

func merge(a, b map[string][]cleaner.CleanupDecision) map[string][]cleaner.CleanupDecision {
	result := make(map[string][]cleaner.CleanupDecision, len(a)+len(b))
	for k, v := range a {
		result[k] = append(result[k], v...)
	}
	for k, v := range b {
		result[k] = append(result[k], v...)
	}
	return result
}
