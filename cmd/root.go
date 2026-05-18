package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
	"github.com/asmild/artifactory-cleaner/internal/cleaner"
	"github.com/asmild/artifactory-cleaner/internal/strategy"
	"github.com/spf13/cobra"
)

var cfgFile string
var dryRun bool
var force bool
var outputFile string
var outputFormat string

var allowedFormats = []string{"table", "csv", "html", "xlsx"}

var rootCmd = &cobra.Command{
	Use:   "artycleaner",
	Short: "Clean up stale artifacts from an Artifactory repository",
	Long: `artycleaner removes stale artifacts from Artifactory based on configurable
retention policies — by age, recent-download window, or explicit whitelists.

The cleanup strategy is auto-detected from the repository's package type
(Docker, Maven, Generic, etc.) via the Artifactory API.

Requires ARTIFACTORY_URL and ARTIFACTORY_TOKEN environment variables.`,

	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		target, _ := cmd.Flags().GetString("target")

		artClient, err := artifactory.New()
		if err != nil {
			return err
		}

		if cfgFile == "" {
			cfgFile = cleaner.ConfigFile
		}
		props, err := cleaner.LoadCleanupProperties(cfgFile)
		if err != nil {
			return err
		}
		settings, ok := props.Cleanup.Repositories[target]
		if !ok {
			return fmt.Errorf("target %q not found in config", target)
		}

		repoInfo, err := artClient.GetRepoInfo(ctx, settings.Name)
		if err != nil {
			return fmt.Errorf("cannot access repository %q: %w", settings.Name, err)
		}
		if repoInfo.RClass != "local" {
			return fmt.Errorf("repository %q is %q — only local repos can be cleaned", settings.Name, repoInfo.RClass)
		}
		fmt.Printf("Repository: %s  type: %s  package: %s\n\n", repoInfo.Key, repoInfo.RClass, repoInfo.PackageType)

		strat, err := strategy.New(repoInfo.PackageType, artClient)
		if err != nil {
			return err
		}

		plan, err := cleaner.NewCleanupPlan(ctx, target, cfgFile, dryRun, artClient, strat)
		if err != nil {
			return err
		}

		resolvedOutput := resolveOutputFile(outputFile, target, outputFormat)
		if err := plan.ShowReport(outputFormat, resolvedOutput); err != nil {
			return err
		}

		plan.PrintCleanupStatistics()

		if plan.Stats.ArtifactsForDeletion == 0 {
			fmt.Println("\nNothing to delete. Exiting.")
			return nil
		}

		if !force {
			var confirm string
			fmt.Print("Delete artifacts. Do you want to proceed? (y/n): ")
			fmt.Scan(&confirm)
			if confirm != "y" && confirm != "Y" {
				fmt.Println("Operation cancelled.")
				return nil
			}
		}

		if dryRun {
			fmt.Println("\nDry-run: artifacts listed above would be deleted.")
		} else {
			fmt.Println("\nDeleting artifacts:")
		}
		return plan.Execute(ctx)
	},
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

// SetVersion sets the version string shown by --version.
func SetVersion(v string) {
	rootCmd.Version = v
}

func init() {
	rootCmd.SilenceUsage = true
	rootCmd.Flags().StringVar(&cfgFile, "config", "", "Cleanup config file (default: "+cleaner.ConfigFile+")")
	rootCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be deleted without deleting")
	rootCmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	rootCmd.Flags().StringP("target", "t", "", "Target repository key from the config file")
	rootCmd.MarkFlagRequired("target") //nolint:errcheck
	rootCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Write report to file instead of stdout")
	rootCmd.Flags().StringVarP(&outputFormat, "format", "f", "table", "Report format: table, csv, html, xlsx")
	cobra.OnInitialize(validateArgs)
}

func validateArgs() {
	if !slices.Contains(allowedFormats, outputFormat) {
		fmt.Fprintf(os.Stderr, "invalid --format %q: allowed values are %v\n", outputFormat, allowedFormats)
		os.Exit(1)
	}
	if dryRun {
		fmt.Println("Dry-run mode: no artifacts will be deleted.")
	}
}

func resolveOutputFile(explicit, target, format string) string {
	if explicit != "" || format == "table" {
		return explicit
	}
	return fmt.Sprintf("report-%s.%s", target, format)
}
