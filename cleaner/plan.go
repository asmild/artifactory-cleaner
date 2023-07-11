package cleaner

import (
	"fmt"
	"github.com/asmild/artifactory-cleaner/aql"
	"github.com/asmild/artifactory-cleaner/util"
	"time"
)

//Context might be used for keeping AQL

func NewCleanupPlan(target string, cfgFile string, dryRun bool) (CleanupPlan, error) {
	var cleanupPlan CleanupPlan
	if len(cfgFile) == 0 {
		cfgFile = ConfigFile
	}

	cleanupProperties, err := loadCleanupProperties(cfgFile)
	if nil != err {
		return cleanupPlan, err
	}

	repositoryProperties, ok := cleanupProperties.Cleanup.Repositories[target]
	if !ok {
		return cleanupPlan, fmt.Errorf("target '%s' not found", target)
	}

	fmt.Printf("Creating cleanup plan for '%s' based on further properties:\n", target)
	fmt.Printf("\t- Repository Name: %s\n", repositoryProperties.Name)
	fmt.Printf("\t- Clean Version Pattern: %s\n", repositoryProperties.CleanVersionPattern)
	fmt.Printf("\t- Path Matcher: %s\n", repositoryProperties.PathMatcher)
	fmt.Printf("\t- Discriminator: %s\n", repositoryProperties.Discriminator)
	fmt.Printf("\t- Recent Artifact Retention: %d\n", repositoryProperties.RecentArtifactRetention)
	fmt.Printf("\t- Last Downloaded Days: %d\n", repositoryProperties.LastDownloadedDays)
	fmt.Printf("\t- Whitelisted Groups: %v\n", repositoryProperties.WhitelistedGroups)
	fmt.Printf("\t- Whitelisted Versions: %v\n", repositoryProperties.WhitelistedVersions)
	fmt.Printf("\t- Whitelisted Artifacts: %v\n", repositoryProperties.WhitelistedArtifacts)
	fmt.Println("")

	fmt.Println("Preparing decisions plan")
	a, err := aql.New()
	if err != nil {
		return cleanupPlan, err
	}
	groupedDecisionMap, err := createUndecidedGroupedDecisionMap(repositoryProperties)
	cleanupStatistics := makeDecisions(&groupedDecisionMap, repositoryProperties)

	if err != nil {
		return cleanupPlan, err
	}

	cleanupPlan = CleanupPlan{
		Repository:         repositoryProperties.Name,
		DryRun:             dryRun,
		Stats:              cleanupStatistics,
		GroupedDecisionMap: groupedDecisionMap,
		Timestamp:          time.Now(),
		AQL:                a,
	}
	return cleanupPlan, nil
}

func (cp *CleanupPlan) Execute() error {
	a, err := aql.New()
	if err != nil {
		return err
	}
	for _, decisions := range cp.GroupedDecisionMap {
		for _, decision := range decisions {
			artifactMetadata := decision.ArtifactMetadata
			// Deleting artifact
			if decision.CleanupAction == 5 {
				fmt.Printf("Deleting %s, %s ...", artifactMetadata.Path, util.FormatSize(artifactMetadata.Size))
				if !cp.DryRun {
					err = a.DeleteArtifact(cp.Repository, artifactMetadata.Path)
					if err != nil {
						return err
					}
					fmt.Printf("Done!\n")
				} else {
					fmt.Printf("Skipped.\n")
				}

			}
		}
	}
	return nil
}
