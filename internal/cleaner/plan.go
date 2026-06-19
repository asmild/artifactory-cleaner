package cleaner

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// NewCleanupPlan validates the target repo, runs the strategy, and returns a
// CleanupPlan ready for reporting and execution.
func NewCleanupPlan(ctx context.Context, target, cfgFile string, dryRun bool, artClient Deleter, strat Strategy, deleteLimit int) (CleanupPlan, error) {
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
	fmt.Printf("\t- Rules:\n")
	for _, r := range settings.Rules {
		fmt.Printf("\t   * Name: %s\n", r.Name)
		val := reflect.ValueOf(r)
		typeOfStruct := val.Type()
		for i := 0; i < val.NumField(); i++ {
			if typeOfStruct.Field(i).Name != "Name" {
				fmt.Printf("\t     %s: %v\n", typeOfStruct.Field(i).Name, val.Field(i).Interface())
			}
		}
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
		DeleteLimit:        deleteLimit,
		Stats:              stats,
		GroupedDecisionMap: decisionMap,
		Timestamp:          time.Now(),
		artClient:          artClient,
	}, nil
}

// Execute carries out the planned deletions (or prints them if DryRun is set).
func (cp *CleanupPlan) Execute(ctx context.Context) error {
	artifactsCount := 0

	for _, decisions := range cp.GroupedDecisionMap {
		for _, decision := range decisions {
			if decision.CleanupAction != DELETE && decision.CleanupAction != DELETE_ORPHANED {
				continue
			}

			var s []string
			artifactsCount++

			if err := ctx.Err(); err != nil {
				return err
			}
			meta := decision.Artifact
			fmt.Printf("Deleting %s (%s)... ", meta.Path, formatSize(meta.Size))

			if cp.DryRun {
				s = append(s, "dry-run")
			}

			if cp.DeleteLimit != 0 && cp.DeleteLimit < artifactsCount {
				s = append(s, "deletion limit-reached")
			}

			if len(s) > 0 {
				fmt.Println("skipped (" + strings.Join(s, ", ") + ").")
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
