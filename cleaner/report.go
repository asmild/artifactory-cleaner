package cleaner

import (
	"encoding/csv"
	"errors"
	"fmt"
	output "github.com/asmild/artifactory-cleaner/util"
	"github.com/olekukonko/tablewriter"
	"html/template"
	"os"
	"time"
)

type CustomTable struct {
	*tablewriter.Table
}

func (cp *CleanupPlan) ShowReport(format string, outputFile string) error {
	switch format {
	case "csv":
		cp.PrintCSV(outputFile)
	case "table":
		cp.PrintTable()
	case "html":
		cp.PrintHtml()
	default:
		return errors.New(fmt.Sprintf("Unsupported format: %s", format))
	}
	return nil
}

func NewCustomTable() *CustomTable {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	//table.SetCenterSeparator("")
	//table.SetColumnSeparator("")
	//table.SetRowSeparator("-")
	//table.SetRowLine(true)
	//table.SetAutoMergeCells(true)
	//table.SetHeaderLine(true)
	//table.SetTablePadding("\t") // pad with tabs
	//table.SetNoWhiteSpace(true)

	return &CustomTable{
		Table: table,
	}
}

func (cp *CleanupPlan) PrintCSV(outputFile string) error {
	writer := &csv.Writer{}
	if len(outputFile) == 0 {
		writer = csv.NewWriter(os.Stdout)
	} else {
		file, err := os.Create(outputFile)
		if err != nil {
			return err
		}
		defer file.Close()
		writer = csv.NewWriter(file)
	}
	defer writer.Flush()

	headers := []string{
		"Group",
		"Path",
		"Version",
		"Size",
		"Created At",
		"Last Downloaded At",
		"Cleanup Action",
	}
	writer.Write(headers)

	for group, decisions := range cp.GroupedDecisionMap {
		for _, decision := range decisions {
			artifactMetadata := decision.ArtifactMetadata

			row := []string{
				group,
				artifactMetadata.Path,
				artifactMetadata.Version,
				output.FormatSize(artifactMetadata.Size),
				datestampFormat(artifactMetadata.CreatedAt),
				datestampFormat(artifactMetadata.LastDownloadedAt),
				CleanupActionStrings[decision.CleanupAction],
			}
			writer.Write(row)
		}
	}
	//writer.Flush()
	if len(outputFile) != 0 {
		fmt.Printf("CSV report written to file '%s' \n", outputFile)
	}
	return nil
}

func (cp *CleanupPlan) PrintTable() {
	table := NewCustomTable()
	table.addTableFooter(cp)
	table.generateCleanUpReportTable(cp)
	table.Render()
}

func (cp *CleanupPlan) PrintHtml() {
	// Populate your data here

	file, err := os.Create("output.html")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	//tmpl := template.Must(template.ParseFiles("templates/template.html"))
	tmpl := template.Must(template.New("template.html").Funcs(template.FuncMap{
		"FormatSize":      output.FormatSize,
		"DatestampFormat": datestampFormat,
	}).ParseFiles("templates/template.html"))

	err = tmpl.Execute(file, struct {
		Data                 *CleanupPlan
		CleanupActionStrings map[CleanupAction]string
	}{
		Data:                 cp,
		CleanupActionStrings: CleanupActionStrings,
	})

	//err := tmpl.Execute(os.Stdout, data)
	if err != nil {
		fmt.Println(err)
	}
}

//func (cp *CleanupPlan) printJSON() {
//	data, err := json.MarshalIndent(cp, "", "  ")
//	if err != nil {
//		fmt.Println("Error marshaling JSON:", err)
//		return
//	}
//
//	fmt.Println(string(data))
//}

func setCellCollor(cleanupAction CleanupAction) []tablewriter.Colors {
	color := tablewriter.FgGreenColor
	if cleanupAction == DELETE {
		color = tablewriter.FgRedColor
	}

	return []tablewriter.Colors{
		tablewriter.Colors{}, // Group
		tablewriter.Colors{}, // Path
		tablewriter.Colors{}, // Version
		tablewriter.Colors{}, // Size
		tablewriter.Colors{}, // Created At
		tablewriter.Colors{}, // Last Downloaded At
		tablewriter.Colors{ // Cleanup Action
			tablewriter.Bold,
			color,
		},
	}
}

func (t *CustomTable) generateCleanUpReportTable(cp *CleanupPlan) {
	t.SetHeader([]string{
		"Group",
		"Path",
		"Version",
		"Size",
		"Created At",
		"Last Downloaded At",
		"Cleanup Action",
	})

	for group, decisions := range cp.GroupedDecisionMap {
		for i, decision := range decisions {
			artifactMetadata := decision.ArtifactMetadata
			groupCell := ""
			if i == 0 {
				groupCell = group
			}

			t.Rich([]string{
				groupCell,                                          // Group
				artifactMetadata.Path,                              // Path
				artifactMetadata.Version,                           // Version
				output.FormatSize(artifactMetadata.Size),           // Size
				datestampFormat(artifactMetadata.CreatedAt),        // Created At
				datestampFormat(artifactMetadata.LastDownloadedAt), // Last Downloaded At
				CleanupActionStrings[decision.CleanupAction],       // Cleanup Action
			}, setCellCollor(decision.CleanupAction),
			)
		}
	}
}

func (t *CustomTable) addTableFooter(cp *CleanupPlan) {
	t.SetFooter([]string{
		"",
		"",
		"",
		"",
		"",
		"",
		fmt.Sprintf("Deleted %d of %d eligible artifacts", cp.Stats.ArtifactsForDeletion, cp.Stats.TotalArtifacts),
	})
	t.SetAlignment(tablewriter.ALIGN_LEFT)
}

func (cp *CleanupPlan) PrintCleanupStatistics() {
	fmt.Printf("Cleanup report:\n%s",
		fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s\n",
			fmt.Sprintf(" DryRun: %t", cp.DryRun),
			fmt.Sprintf(" Repository: %s", cp.Repository),
			fmt.Sprintf(" Delete %d of %d eligible artifacts", cp.Stats.ArtifactsForDeletion, cp.Stats.TotalArtifacts),
			fmt.Sprintf(" Artifacts to remove: %d", cp.Stats.ArtifactsForDeletion),
			fmt.Sprintf(" Whitelisted artifacts: %d", cp.Stats.ArtifactsWhitelisted),
			fmt.Sprintf(" Estimation of freed space: %s", output.FormatSize(cp.Stats.TotalSizeForDeletion)),
		),
	)
}
func datestampFormat(timestamp *time.Time) string {
	if timestamp != nil {
		isoFormat := "2006-01-02 15:04:05"
		return timestamp.Format(isoFormat)
	}
	return "- never -"
}
