// Package strategy provides package-type-specific cleanup strategies.
// The correct strategy is selected automatically from the repository's
// packageType returned by the Artifactory API.
package strategy

import (
	"fmt"
	"strings"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
	"github.com/asmild/artifactory-cleaner/internal/cleaner"
	"github.com/asmild/artifactory-cleaner/internal/strategy/docker"
	"github.com/asmild/artifactory-cleaner/internal/strategy/generic"
	"github.com/asmild/artifactory-cleaner/internal/strategy/maven"
)

// New returns the CleanupStrategy appropriate for the given Artifactory packageType.
// Comparison is case-insensitive — Artifactory returns lowercase values (e.g. "docker").
func New(packageType string, client *artifactory.Client) (cleaner.Strategy, error) {
	switch strings.ToLower(packageType) {
	case "docker":
		return docker.New(client), nil
	case "maven", "gradle", "ivy", "sbt":
		return maven.New(client), nil
	case "npm", "pypi", "nuget", "helm", "go", "cargo", "rubygems", "generic":
		return generic.New(client), nil
	default:
		return nil, fmt.Errorf(
			"unsupported package type %q — add a discriminator and use the generic strategy, or implement a dedicated strategy",
			packageType,
		)
	}
}
