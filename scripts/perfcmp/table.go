package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type TableRow struct {
	Epoch      int
	GroupStats []APIResponse // one per group
}

// PrintTable prints the results table to stdout
func PrintTable(table []TableRow, committees [][][]int) {
	headers := []string{"Epoch"}
	intervals := make([]string, len(committees))
	for i, group := range committees {
		intervals[i] = CommitteeGroupToIntervalString(group)
		headers = append(headers, fmt.Sprintf("%s Effectiveness", intervals[i]))
		headers = append(headers, fmt.Sprintf("%s Correctness", intervals[i]))
	}
	if len(committees) == 2 {
		headers = append(headers, "Effectiveness Diff", "Correctness Diff")
	}

	fmt.Println()
	fmt.Println(strings.Join(headers, " | "))
	fmt.Println(strings.Repeat("-", 16*len(headers)))
	for _, row := range table {
		fields := []string{strconv.Itoa(row.Epoch)}
		for _, stats := range row.GroupStats {
			fields = append(fields,
				fmt.Sprintf("%.4f", stats.Effectiveness),
				fmt.Sprintf("%.4f", stats.Correctness),
			)
		}
		if len(row.GroupStats) == 2 {
			effDiff := row.GroupStats[0].Effectiveness - row.GroupStats[1].Effectiveness
			corrDiff := row.GroupStats[0].Correctness - row.GroupStats[1].Correctness
			fields = append(fields,
				fmt.Sprintf("%.4f", effDiff),
				fmt.Sprintf("%.4f", corrDiff),
			)
		}
		fmt.Println(strings.Join(fields, " | "))
	}
}

// ExportTableCSV writes the table to a CSV file (or stdout if filename is empty)
func ExportTableCSV(table []TableRow, committees [][][]int, filename string) error {
	headers := []string{"Epoch"}
	intervals := make([]string, len(committees))
	for i, group := range committees {
		intervals[i] = CommitteeGroupToIntervalString(group)
		headers = append(headers, fmt.Sprintf("%s Effectiveness", intervals[i]))
		headers = append(headers, fmt.Sprintf("%s Correctness", intervals[i]))
	}
	if len(committees) == 2 {
		headers = append(headers, "Effectiveness Diff", "Correctness Diff")
	}

	var file *os.File
	var err error
	if filename == "" {
		file = os.Stdout
	} else {
		file, err = os.Create(filename)
		if err != nil {
			return err
		}
		defer file.Close()
	}
	w := csv.NewWriter(file)
	defer w.Flush()

	if err := w.Write(headers); err != nil {
		return err
	}

	for _, row := range table {
		fields := []string{strconv.Itoa(row.Epoch)}
		for _, stats := range row.GroupStats {
			fields = append(fields,
				fmt.Sprintf("%.4f", stats.Effectiveness),
				fmt.Sprintf("%.4f", stats.Correctness),
			)
		}
		if len(row.GroupStats) == 2 {
			effDiff := row.GroupStats[0].Effectiveness - row.GroupStats[1].Effectiveness
			corrDiff := row.GroupStats[0].Correctness - row.GroupStats[1].Correctness
			fields = append(fields,
				fmt.Sprintf("%.4f", effDiff),
				fmt.Sprintf("%.4f", corrDiff),
			)
		}
		if err := w.Write(fields); err != nil {
			return err
		}
	}
	return nil
}
