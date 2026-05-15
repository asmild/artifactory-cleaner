// Package maven implements the cleanup strategy for Maven (and Gradle/Ivy/SBT)
// repositories. Artifacts are fetched by discriminator (default: *.pom) and
// grouped by artifactId path for retention rule evaluation.
package maven

import (
	"context"
	"fmt"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
	"github.com/asmild/artifactory-cleaner/internal/cleaner"
)

const defaultDiscriminator = "*.pom"
const defaultPathMatcher = "*"

type Strategy struct {
	client artifactory.Repository
}

func New(client artifactory.Repository) *Strategy {
	return &Strategy{client: client}
}

func (s *Strategy) Plan(ctx context.Context, settings cleaner.TargetSettings) (map[string][]cleaner.CleanupDecision, error) {
	discriminator, pathMatcher := resolveDiscriminator(settings)

	artifacts, err := s.client.FindArtifacts(ctx, artifactory.ArtifactFilter{
		Repo:      settings.Name,
		Name:      discriminator,
		PathMatch: pathMatcher,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch maven artifacts: %w", err)
	}

	dm := cleaner.BuildDecisionMap(artifacts)
	cleaner.MakeDecisions(dm, settings)
	return dm, nil
}

func resolveDiscriminator(settings cleaner.TargetSettings) (discriminator, pathMatcher string) {
	for _, rule := range settings.Rules {
		if rule.Discriminator != "" {
			p := rule.PathMatcher
			if p == "" {
				p = defaultPathMatcher
			}
			return rule.Discriminator, p
		}
	}
	return defaultDiscriminator, defaultPathMatcher
}
