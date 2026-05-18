package cleaner

import (
	"encoding/csv"
	"errors"
	"fmt"
	"html/template"
	"os"
	"sort"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/xuri/excelize/v2"
)

// ShowReport renders the cleanup plan in the requested format.
func (cp *CleanupPlan) ShowReport(format, outputFile string) error {
	switch format {
	case "csv":
		return cp.printCSV(outputFile)
	case "table":
		cp.printTable()
		return nil
	case "html":
		return cp.printHTML(outputFile)
	case "xlsx":
		return cp.printXLSX(outputFile)
	default:
		return errors.New("unsupported format: " + format)
	}
}

// PrintCleanupStatistics writes a summary to stdout.
func (cp *CleanupPlan) PrintCleanupStatistics() {
	// Count per action.
	counts := make(map[CleanupAction]int64)
	for _, decisions := range cp.GroupedDecisionMap {
		for _, d := range decisions {
			counts[d.CleanupAction]++
		}
	}

	fmt.Printf("Cleanup report:\n")
	fmt.Printf("  DryRun:      %t\n", cp.DryRun)
	fmt.Printf("  Repository:  %s\n", cp.Repository)
	fmt.Printf("  Scanned:     %d\n\n", cp.Stats.TotalArtifacts)

	order := []CleanupAction{
		RECENT_VERSION, DOWNLOADED_RECENTLY, CREATED_RECENTLY,
		WHITELISTED, PROTECTED, MANIFEST_LIST_REF, UNMATCHED_KEEP,
		DELETE,
	}
	for _, a := range order {
		if n := counts[a]; n > 0 {
			fmt.Printf("  %-22s %d\n", CleanupActionStrings[a]+":", n)
		}
	}
	fmt.Printf("\n  Estimated space freed: %s\n", formatSize(cp.Stats.TotalSizeForDeletion))
	fmt.Printf("\n  Action legend:\n")
	fmt.Printf("    RECENT_VERSION      — kept: within retention count for its rule\n")
	fmt.Printf("    DOWNLOADED_RECENTLY — kept: downloaded within the rule's lastDownloadedDays window\n")
	fmt.Printf("    WHITELISTED         — kept: in the matched rule's whitelist\n")
	fmt.Printf("    PROTECTED           — kept: in target protectedVersions or protectedGroups (checked before rules)\n")
	fmt.Printf("    CREATED_RECENTLY    — kept: created within the rule's artifactLifetimeDays grace period\n")
	fmt.Printf("    MANIFEST_LIST_REF   — kept: Docker platform image (sha256) referenced by a\n")
	fmt.Printf("                          retained manifest list; deleting it would break docker pull\n")
	fmt.Printf("    UNMATCHED_KEEP      — kept: no rule pattern matched, unmatchedAction is 'keep'\n")
	fmt.Printf("    DELETE              — will be removed\n")
}

func (cp *CleanupPlan) printCSV(outputFile string) error {
	var w *csv.Writer
	if outputFile == "" {
		w = csv.NewWriter(os.Stdout)
	} else {
		f, err := os.Create(outputFile)
		if err != nil {
			return err
		}
		defer f.Close()
		w = csv.NewWriter(f)
	}
	defer w.Flush()

	_ = w.Write([]string{"Group", "Path", "Version", "Tag", "Size", "Created At", "Last Downloaded At", "Cleanup Action"})

	for group, decisions := range cp.GroupedDecisionMap {
		for _, d := range decisions {
			m := d.Artifact
			_ = w.Write([]string{
				group,
				m.Path,
				m.Version,
				m.ManifestListTag,
				formatSize(m.Size),
				formatTimestamp(m.CreatedAt),
				formatTimestamp(m.LastDownloadedAt),
				CleanupActionStrings[d.CleanupAction],
			})
		}
	}

	if outputFile != "" {
		fmt.Printf("CSV report written to %q\n", outputFile)
	}
	return nil
}

func (cp *CleanupPlan) printTable() {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetHeader([]string{"Group", "Path", "Version", "Tag", "Size", "Created At", "Last Downloaded At", "Cleanup Action"})
	table.SetFooter([]string{
		"", "", "", "", "", "", "",
		fmt.Sprintf("Delete %d of %d artifacts", cp.Stats.ArtifactsForDeletion, cp.Stats.TotalArtifacts),
	})

	for group, decisions := range cp.GroupedDecisionMap {
		for i, d := range decisions {
			m := d.Artifact
			groupCell := ""
			if i == 0 {
				groupCell = group
			}
			color := tablewriter.FgGreenColor
			if d.CleanupAction == DELETE {
				color = tablewriter.FgRedColor
			}
			table.Rich(
				[]string{
					groupCell,
					m.Path,
					m.Version,
					m.ManifestListTag,
					formatSize(m.Size),
					formatTimestamp(m.CreatedAt),
					formatTimestamp(m.LastDownloadedAt),
					CleanupActionStrings[d.CleanupAction],
				},
				[]tablewriter.Colors{
					{}, {}, {}, {}, {}, {}, {},
					{tablewriter.Bold, color},
				},
			)
		}
	}
	table.Render()
}

func (cp *CleanupPlan) printHTML(outputFile string) error {
	if outputFile == "" {
		outputFile = "output.html"
	}

	f, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer f.Close()

	tmpl, err := template.New("template.html").Funcs(template.FuncMap{
		"FormatSize":      formatSize,
		"DatestampFormat": formatTimestamp,
	}).ParseFiles("templates/template.html")
	if err != nil {
		return fmt.Errorf("parse HTML template: %w", err)
	}

	if err := tmpl.Execute(f, struct {
		Data                 *CleanupPlan
		CleanupActionStrings map[CleanupAction]string
	}{cp, CleanupActionStrings}); err != nil {
		return fmt.Errorf("render HTML template: %w", err)
	}

	fmt.Printf("HTML report written to %q\n", outputFile)
	return nil
}

func (cp *CleanupPlan) printXLSX(outputFile string) error {
	if outputFile == "" {
		outputFile = "report.xlsx"
	}

	f := excelize.NewFile()
	defer f.Close()

	// ── styles ────────────────────────────────────────────────────────────────
	// Colors are in RRGGBB format (6 digits). Alpha prefix omitted to ensure
	// correct rendering across Excel, Numbers, and LibreOffice.
	mkFill := func(rrggbb string) int {
		s, _ := f.NewStyle(&excelize.Style{
			Fill: excelize.Fill{Type: "pattern", Color: []string{rrggbb}, Pattern: 1},
		})
		return s
	}
	mkHeaderStyle := func() int {
		s, _ := f.NewStyle(&excelize.Style{
			Font: &excelize.Font{Bold: true},
			Fill: excelize.Fill{Type: "pattern", Color: []string{"D9D9D9"}, Pattern: 1},
		})
		return s
	}

	styleHeader      := mkHeaderStyle()
	styleDefault     := mkFill("FFFFFF") // white — applied to all uncoloured rows
	styleDelete      := mkFill("FF9999") // light red
	styleManifestRef := mkFill("B3D9FF") // light blue
	styleKept        := mkFill("99FF99") // light green
	styleProtected   := mkFill("FFFFCC") // light yellow
	styleUnmatched   := mkFill("E8E8E8") // light grey

	actionStyle := func(a CleanupAction) int {
		switch a {
		case DELETE:
			return styleDelete
		case MANIFEST_LIST_REF:
			return styleManifestRef
		case RECENT_VERSION, DOWNLOADED_RECENTLY, WHITELISTED, CREATED_RECENTLY:
			return styleKept
		case PROTECTED:
			return styleProtected
		case UNMATCHED_KEEP:
			return styleUnmatched
		default:
			return styleDefault
		}
	}

	// ── Sheet 1: Report ───────────────────────────────────────────────────────
	const sheet = "Report"
	f.SetSheetName("Sheet1", sheet)

	headers := []string{"Group", "Path", "Version", "Tag", "Size", "Created At", "Last Downloaded At", "Cleanup Action"}
	for col, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		f.SetCellValue(sheet, cell, h)
		f.SetCellStyle(sheet, cell, cell, styleHeader)
	}

	// Sort groups for stable output.
	groups := make([]string, 0, len(cp.GroupedDecisionMap))
	for g := range cp.GroupedDecisionMap {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	row := 2
	for _, group := range groups {
		for _, d := range cp.GroupedDecisionMap[group] {
			m := d.Artifact
			values := []interface{}{
				group,
				m.Path,
				m.Version,
				m.ManifestListTag,
				formatSize(m.Size),
				formatTimestamp(m.CreatedAt),
				formatTimestamp(m.LastDownloadedAt),
				CleanupActionStrings[d.CleanupAction],
			}
			style := actionStyle(d.CleanupAction)
			for col, v := range values {
				cell, _ := excelize.CoordinatesToCellName(col+1, row)
				f.SetCellValue(sheet, cell, v)
				f.SetCellStyle(sheet, cell, cell, style)
			}
			row++
		}
	}

	// Freeze header row and enable auto-filter.
	f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	lastDataCell, _ := excelize.CoordinatesToCellName(len(headers), row-1)
	f.AutoFilter(sheet, "A1:"+lastDataCell, nil)

	// Set column widths.
	colWidths := []float64{30, 55, 30, 20, 12, 20, 20, 20}
	for i, w := range colWidths {
		col, _ := excelize.ColumnNumberToName(i + 1)
		f.SetColWidth(sheet, col, col, w)
	}

	// ── Sheet 2: Summary ──────────────────────────────────────────────────────
	const sumSheet = "Summary"
	f.NewSheet(sumSheet)

	summaryStyle, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})

	summaryRows := [][]interface{}{
		{"Repository", cp.Repository},
		{"DryRun", fmt.Sprintf("%t", cp.DryRun)},
		{"Timestamp", cp.Timestamp.Format("2006-01-02 15:04:05")},
		{},
		{"Artifacts scanned", cp.Stats.TotalArtifacts},
		{"Artifacts to delete", cp.Stats.ArtifactsForDeletion},
		{"Artifacts whitelisted", cp.Stats.ArtifactsWhitelisted},
		{"Estimated space freed", formatSize(cp.Stats.TotalSizeForDeletion)},
		{},
		{"Action", "Meaning"},
		{"RECENT_VERSION", "Kept: within recentArtifactRetention count for its rule"},
		{"DOWNLOADED_RECENTLY", "Kept: downloaded within the rule's lastDownloadedDays window"},
		{"WHITELISTED", "Kept: in the matched rule's whitelist"},
		{"PROTECTED", "Kept: in target protectedVersions or protectedGroups — checked before any rule"},
		{"CREATED_RECENTLY", "Kept: created within the rule's artifactLifetimeDays grace period — safety net for new artifacts not yet downloaded"},
		{"MANIFEST_LIST_REF", "Kept: Docker platform image (sha256) referenced by a retained manifest list — deleting it would break docker pull"},
		{"UNMATCHED_KEEP", "Kept: no rule pattern matched, unmatchedAction is 'keep'"},
		{"DELETE", "Will be removed"},
	}

	for r, rowData := range summaryRows {
		for c, v := range rowData {
			if v == nil {
				continue
			}
			cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
			f.SetCellValue(sumSheet, cell, v)
			if c == 0 && v != "" {
				f.SetCellStyle(sumSheet, cell, cell, summaryStyle)
			}
		}
	}
	f.SetColWidth(sumSheet, "A", "A", 25)
	f.SetColWidth(sumSheet, "B", "B", 80)

	if idx, err := f.GetSheetIndex(sheet); err == nil {
		f.SetActiveSheet(idx)
	}

	if err := f.SaveAs(outputFile); err != nil {
		return fmt.Errorf("save xlsx: %w", err)
	}
	fmt.Printf("XLSX report written to %q\n", outputFile)
	return nil
}

func formatSize(size int64) string {
	const (
		kb = int64(1024)
		mb = kb * 1024
	)
	switch {
	case size >= mb:
		return fmt.Sprintf("%.2f MB", float64(size)/float64(mb))
	case size >= kb:
		return fmt.Sprintf("%.2f KB", float64(size)/float64(kb))
	default:
		return fmt.Sprintf("%d B", size)
	}
}

func formatTimestamp(t *time.Time) string {
	if t == nil {
		return "- never -"
	}
	return t.Format("2006-01-02 15:04:05")
}