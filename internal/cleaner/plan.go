package cleaner

import (
	"context"
	"fmt"
	"time"
)

// NewCleanupPlan validates the target repo, runs the strategy, and returns a
// CleanupPlan ready for reporting and execution.
func NewCleanupPlan(ctx context.Context, target, cfgFile string, dryRun bool, artClient Deleter, strat Strategy) (CleanupPlan, error) {
	var plan CleanupPlan

	if cfgFile == "" {
		cfgFile = ConfigFile
	}

	props, err := LoadCleanupProperties(cfgFile)
	if err != nil {
		return plan, err
	}

	settings, ok := props.Cleanup.Repositories[target]
	if !ok {
		return plan, fmt.Errorf("target %q not found in config", target)
	}

	if warnings := ValidateSettings(target, settings); len(warnings) > 0 {
		PrintWarnings(target, warnings)
	}

	fmt.Printf("Creating cleanup plan for %q:\n", target)
	fmt.Printf("\t- Repository:          %s\n", settings.Name)
	fmt.Printf("\t- Unmatched action:    %s\n", unmatchedLabel(settings))
	fmt.Printf("\t- Protected versions:  %v\n", settings.ProtectedVersions)
	fmt.Printf("\t- Protected groups:    %v\n", settings.ProtectedGroups)
	for _, r := range settings.Rules {
		fmt.Printf("\t- Rule %-20s pattern=%-40s retention=%d days=%d\n",
			fmt.Sprintf("[%s]", r.Name), r.Pattern, r.RecentArtifactRetention, r.LastDownloadedDays)
	}

	fmt.Println()

	fmt.Println("Running cleanup strategy...")
	decisionMap, err := strat.Plan(ctx, settings)
	if err != nil {
		return plan, err
	}

	stats := ComputeStats(decisionMap)

	return CleanupPlan{
		Repository:         settings.Name,
		DryRun:             dryRun,
		Stats:              stats,
		GroupedDecisionMap: decisionMap,
		Timestamp:          time.Now(),
		artClient:          artClient,
	}, nil
}

// Execute carries out the planned deletions (or prints them if DryRun is set).
func (cp *CleanupPlan) Execute(ctx context.Context) error {
	for _, decisions := range cp.GroupedDecisionMap {
		for _, decision := range decisions {
			if decision.CleanupAction != DELETE && decision.CleanupAction != DELETE_ORPHANED {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			meta := decision.Artifact
			fmt.Printf("Deleting %s (%s)... ", meta.Path, formatSize(meta.Size))
			if cp.DryRun {
				fmt.Println("skipped (dry-run).")
				continue
			}
			if err := cp.artClient.DeletePath(ctx, cp.Repository, meta.Path); err != nil {
				return err
			}
			fmt.Println("done.")
		}
	}
	return nil
}

func unmatchedLabel(s TargetSettings) string {
	if s.unmatchedIsDelete() {
		return "delete"
	}
	return "keep (default)"
}
