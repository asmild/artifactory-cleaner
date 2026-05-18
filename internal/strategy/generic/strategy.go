// Package generic implements a configurable cleanup strategy for untyped
// or unsupported repository types. A discriminator filename pattern must be
// provided in the config (e.g. "*.zip"). Artifacts are grouped by parent path.
package generic

import (
	"context"
	"fmt"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
	"github.com/asmild/artifactory-cleaner/internal/cleaner"
)

type Strategy struct {
	client artifactory.Repository
}

func New(client artifactory.Repository) *Strategy {
	return &Strategy{client: client}
}

func (s *Strategy) Plan(ctx context.Context, settings cleaner.TargetSettings) (map[string][]cleaner.CleanupDecision, error) {
	discriminator, pathMatcher := "", "*"
	for _, rule := range settings.Rules {
		if rule.Discriminator != "" {
			discriminator = rule.Discriminator
			if rule.PathMatcher != "" {
				pathMatcher = rule.PathMatcher
			}
			break
		}
	}
	if discriminator == "" {
		return nil, fmt.Errorf("repo %q (Generic): at least one rule must define a discriminator", settings.Name)
	}

	artifacts, err := s.client.FindArtifacts(ctx, artifactory.ArtifactFilter{
		Repo:      settings.Name,
		Name:      discriminator,
		PathMatch: pathMatcher,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch generic artifacts: %w", err)
	}

	dm := cleaner.BuildDecisionMap(artifacts)
	cleaner.MakeDecisions(dm, settings)
	return dm, nil
}
