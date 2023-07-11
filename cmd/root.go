package cmd

import (
	"fmt"
	"github.com/asmild/artifactory-cleaner/cleaner"
	"github.com/asmild/artifactory-cleaner/util"
	"github.com/spf13/cobra"
	"os"
)

var lastDownloadedDays int
var target string
var cfgFile string

var dryRun bool
var force bool

var outputFile string
var outputFormat string
var outputFormatAllowedValues = []string{"table", "csv", "html"}

var silent bool

var rootCmd = &cobra.Command{
	Use:   "artycleaner",
	Short: "A brief description of your application",
	Long: `A longer description that spans multiple lines and likely contains
examples and usage of using your application. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,

	RunE: func(cmd *cobra.Command, args []string) error {
		plan, err := cleaner.NewCleanupPlan(target, cfgFile, dryRun)
		if nil != err {
			return err
		}

		err = plan.ShowReport(outputFormat, outputFile)
		if nil != err {
			return err
		}

		plan.PrintCleanupStatistics()
		if plan.Stats.ArtifactsForDeletion == 0 {
			fmt.Printf("\nNothing to delete. Exiting...\n")
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
			fmt.Printf("\nThese artifacts are not deleted due to dryRun set to true:\n")
		} else {
			fmt.Printf("\nThese artifacts are deleted removed:\n")
		}
		plan.Execute()
		return nil
	},
}

func Execute() error {
	return rootCmd.Execute()
}

//-DdryRun=true
//-DlastDownloadedDays=
//-Dtarget=dockerIotSnapshot
//-Dcleanup.target=dockerIotSnapshot
//-Dcleanup.last.downloaded.days=
//-Dcleanup.dryRun=true

func init() {
	rootCmd.SilenceUsage = true
	rootCmd.Flags().StringVar(&cfgFile, "config", "", "Cleanup config file")
	rootCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Dry run")
	rootCmd.Flags().BoolVarP(&silent, "silent", "s", false, "Silent mode")
	rootCmd.Flags().BoolVar(&force, "force", false, "Omit prompt of confirmation from user")
	rootCmd.Flags().IntVar(&lastDownloadedDays, "lastDownloadedDays", 0, "How long ago the deleting artifact ...")
	rootCmd.Flags().StringVarP(&target, "target", "t", "", " Target repository to clean")
	rootCmd.MarkFlagRequired("target")
	rootCmd.Flags().StringVarP(&outputFile, "output", "o", "", " Write to file instead of stdout")
	rootCmd.Flags().StringVarP(&outputFormat, "format", "f", "table", " Output format - table, csv, html")
	cobra.OnInitialize(validateArgs)
}

func validateArgs() {
	if !util.Contains(outputFormatAllowedValues, outputFormat) {
		fmt.Fprintln(os.Stderr, "invalid value for 'format': allowed values are ", outputFormatAllowedValues)
		os.Exit(1)
	}

	if dryRun {
		fmt.Println("Dry run. Deleting artifacts is not performing")
	}

	if len(outputFile) == 0 {
		fmt.Println("Output file is not defined. Printing to stdout")
	}
}
