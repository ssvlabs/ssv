package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
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

// PrintAverageTable prints a single-row summary for a range
func PrintAverageTable(responses []APIResponse, committees [][][]int, from, to int) {
	headers := []string{"Range"}
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
	fields := []string{fmt.Sprintf("%d-%d", from, to)}
	for _, stats := range responses {
		fields = append(fields,
			fmt.Sprintf("%.4f", stats.Effectiveness),
			fmt.Sprintf("%.4f", stats.Correctness),
		)
	}
	if len(responses) == 2 {
		effDiff := responses[0].Effectiveness - responses[1].Effectiveness
		corrDiff := responses[0].Correctness - responses[1].Correctness
		fields = append(fields,
			fmt.Sprintf("%.4f", effDiff),
			fmt.Sprintf("%.4f", corrDiff),
		)
	}
	fmt.Println(strings.Join(fields, " | "))
}

// ExportAverageCSV writes a single-row summary for a range to CSV
func ExportAverageCSV(responses []APIResponse, committees [][][]int, from, to int, filename string) error {
	headers := []string{"Range"}
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

	fields := []string{fmt.Sprintf("%d-%d", from, to)}
	for _, stats := range responses {
		fields = append(fields,
			fmt.Sprintf("%.4f", stats.Effectiveness),
			fmt.Sprintf("%.4f", stats.Correctness),
		)
	}
	if len(responses) == 2 {
		effDiff := responses[0].Effectiveness - responses[1].Effectiveness
		corrDiff := responses[0].Correctness - responses[1].Correctness
		fields = append(fields,
			fmt.Sprintf("%.4f", effDiff),
			fmt.Sprintf("%.4f", corrDiff),
		)
	}
	if err := w.Write(fields); err != nil {
		return err
	}
	return nil
}

// ExportPlotHTML generates an HTML file with two Chart.js line charts: Effectiveness and Correctness
func ExportPlotHTML(table []TableRow, committees [][][]int, filename string) error {
	if filename == "" {
		filename = "plot.html"
	}
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Prepare data for JS
	epochs := make([]int, len(table))
	for i, row := range table {
		epochs[i] = row.Epoch
	}
	intervals := make([]string, len(committees))
	for i, group := range committees {
		intervals[i] = CommitteeGroupToIntervalString(group)
	}
	effData := make([][]float64, len(committees))
	corrData := make([][]float64, len(committees))
	minEff, minCorr := 1.0, 1.0
	for i := range committees {
		effData[i] = make([]float64, len(table))
		corrData[i] = make([]float64, len(table))
		for j, row := range table {
			eff := row.GroupStats[i].Effectiveness
			corr := row.GroupStats[i].Correctness
			effData[i][j] = eff
			corrData[i][j] = corr
			if eff < minEff {
				minEff = eff
			}
			if corr < minCorr {
				minCorr = corr
			}
		}
	}
	// Set y-axis min to 0.95 or slightly below min value
	minEffY := math.Min(0.95, minEff-0.01)
	minCorrY := math.Min(0.95, minCorr-0.01)

	// Marshal data to JSON for embedding
	epochsJSON, _ := json.Marshal(epochs)
	intervalsJSON, _ := json.Marshal(intervals)
	effDataJSON, _ := json.Marshal(effData)
	corrDataJSON, _ := json.Marshal(corrData)

	html := `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Performance Charts</title>
  <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
</head>
<body>
  <h2>Effectiveness per Epoch</h2>
  <canvas id="effChart" width="1200" height="400"></canvas>
  <h2>Correctness per Epoch</h2>
  <canvas id="corrChart" width="1200" height="400"></canvas>
  <script>
    const epochs = ` + string(epochsJSON) + `;
    const intervals = ` + string(intervalsJSON) + `;
    const effData = ` + string(effDataJSON) + `;
    const corrData = ` + string(corrDataJSON) + `;
    const minEffY = ` + fmt.Sprintf("%.4f", minEffY) + `;
    const minCorrY = ` + fmt.Sprintf("%.4f", minCorrY) + `;
    const colors = [
      'rgba(54, 162, 235, 1)',
      'rgba(255, 99, 132, 1)',
      'rgba(255, 206, 86, 1)',
      'rgba(75, 192, 192, 1)'
    ];
    // Effectiveness chart
    const effDatasets = [];
    for (let i = 0; i < intervals.length; i++) {
      effDatasets.push({
        label: intervals[i],
        data: effData[i],
        borderColor: colors[i % colors.length],
        backgroundColor: colors[i % colors.length],
        fill: false,
        tension: 0.1,
        yAxisID: 'y',
      });
    }
    new Chart(document.getElementById('effChart').getContext('2d'), {
      type: 'line',
      data: {
        labels: epochs,
        datasets: effDatasets
      },
      options: {
        responsive: false,
        plugins: {
          legend: { position: 'top' },
          title: { display: true, text: 'Effectiveness per Epoch' }
        },
        scales: {
          y: {
            min: minEffY,
            max: 1,
            title: { display: true, text: 'Effectiveness' }
          }
        }
      }
    });
    // Correctness chart
    const corrDatasets = [];
    for (let i = 0; i < intervals.length; i++) {
      corrDatasets.push({
        label: intervals[i],
        data: corrData[i],
        borderColor: colors[i % colors.length],
        backgroundColor: colors[i % colors.length],
        fill: false,
        tension: 0.1,
        yAxisID: 'y',
      });
    }
    new Chart(document.getElementById('corrChart').getContext('2d'), {
      type: 'line',
      data: {
        labels: epochs,
        datasets: corrDatasets
      },
      options: {
        responsive: false,
        plugins: {
          legend: { position: 'top' },
          title: { display: true, text: 'Correctness per Epoch' }
        },
        scales: {
          y: {
            min: minCorrY,
            max: 1,
            title: { display: true, text: 'Correctness' }
          }
        }
      }
    });
  </script>
</body>
</html>
`
	_, err = file.WriteString(html)
	return err
}
