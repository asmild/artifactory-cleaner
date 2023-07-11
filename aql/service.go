package aql

import (
	"fmt"
	"github.com/schollz/progressbar/v3"
	"strings"
	"time"
)

func (c *AQL) GetArtifacts(target string, pathMatcher string, discriminator string) ([]ArtifactMetadata, error) {
	allArtifacts, err := c.fetchArtifactMetadata(target, pathMatcher, discriminator)

	if err != nil {
		return nil, err
	}

	return allArtifacts, nil
}

func (c *AQL) DeleteArtifact(repo string, path string) error {
	return c.deleteArtifact(repo, path)
}

func (c *AQL) fetchArtifactMetadata(target string, pathMatcher string, discriminator string) ([]ArtifactMetadata, error) {
	fmt.Printf("Fetching artifacts from repository '%s'... ", target)
	artifactViews, err := c.fetchArtifacts(target, pathMatcher, discriminator)
	if err != nil {
		return nil, fmt.Errorf("fetching artifacts failed: \n\t %s", err)
	}

	fmt.Printf("Found %d artifacts. Getting metadata:\n", len(artifactViews))
	var artifacts []ArtifactMetadata

	bar := progressbar.New(len(artifactViews))
	i := 0
	for _, view := range artifactViews {
		bar.Add(1)
		artifact, err := c.toArtifactMetadata(target, view)

		// Simple bar
		//fmt.Print("\b|")
		//switch i % 4 {
		//case 0:
		//	fmt.Print("/")
		//case 1:
		//	fmt.Print("-")
		//case 2:
		//	fmt.Print("\\")
		//case 3:
		//	fmt.Print("|")
		//}

		// On debug purpose
		//if i > 100 {
		//	break
		//}

		i++

		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	fmt.Printf("\n\n")
	return artifacts, nil
}

func (c *AQL) toArtifactMetadata(repository string, artifactView ArtifactView) (ArtifactMetadata, error) {
	path := artifactView.Path
	children, err := c.getArtifactChildren(repository, path)
	if err != nil {
		return ArtifactMetadata{}, err
	}

	var size int64
	for _, child := range children {
		size += child.Size
	}

	splitIndex := strings.LastIndex(path, "/")

	group := ""
	if splitIndex > 0 {
		group = path[:splitIndex]
	}

	version := ""
	if splitIndex+1 < len(path) {
		version = path[splitIndex+1:]
	}

	var lastDownloadedAt *time.Time
	if len(artifactView.Stats) > 0 {
		downloaded := artifactView.Stats[0].Downloaded
		lastDownloadedAt = &downloaded
	}

	return ArtifactMetadata{
		Path:             path,
		Group:            group,
		Version:          version,
		Size:             size,
		CreatedAt:        &artifactView.Created,
		LastUpdatedAt:    &artifactView.Updated,
		LastDownloadedAt: lastDownloadedAt,
	}, nil
}
