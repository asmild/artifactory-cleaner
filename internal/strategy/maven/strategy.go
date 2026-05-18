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

// resolveDiscriminator returns the discriminator and pathMatcher to use for the
// AQL fetch from the first rule, falling back to defaults (*.pom and *).
// pathMatcher is honoured even when using the default discriminator, so rules
// can scope the AQL to a subset of the repo (e.g. "*infobip*").
func resolveDiscriminator(settings cleaner.TargetSettings) (discriminator, pathMatcher string) {
	discriminator = defaultDiscriminator
	pathMatcher = defaultPathMatcher
	if len(settings.Rules) > 0 {
		r := settings.Rules[0]
		if r.Discriminator != "" {
			discriminator = r.Discriminator
		}
		if r.PathMatcher != "" {
			pathMatcher = r.PathMatcher
		}
	}
	return
}
