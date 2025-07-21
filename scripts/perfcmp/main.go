package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// To use authentication, create a .env file in scripts/perfcmp/ with:
// VAULT_AUTH_COOKIE=your_cookie_value

func main() {
	var (
		epochsFlag     = flag.String("epochs", "", "Epoch(s) to analyze, e.g. 1,2,3 or 1-4 or 1-2,4-5 or 10-20,25-30;40-70 for multi-period avg")
		committeesFlag = flag.String("committees", "", "Committees to compare, e.g. 1,2,3,4;5,6,7,8+9,10,11,12 or 1-4;5-12")
		baseURLFlag    = flag.String("base-url", "https://e2m-hoodi.stage.ops.ssvlabsinternal.com/api/stats", "Base URL for dashboard API")
		cookieFlag     = flag.String("cookie", "", "vaultAuthCookie value (optional, can also be set in .env as VAULT_AUTH_COOKIE)")
		csvFlag        = flag.String("csv", "", "CSV output file (optional, if not set, prints to stdout)")
		averageFlag    = flag.Bool("average", false, "If set, fetch and print/export only the average for the range (requires --epochs to be a single range or multi-period)")
		avgFlag        = flag.Bool("avg", false, "Alias for --average")
		plotFlag       = flag.String("plot", "", "HTML output file for line chart (optional, if not set, uses plot.html)")
	)
	flag.Parse()

	_ = godotenv.Load("scripts/perfcmp/.env")

	cookie := *cookieFlag
	if cookie == "" {
		cookie = os.Getenv("VAULT_AUTH_COOKIE")
	}
	if cookie != "" {
		cookie = "vaultAuthCookie=" + cookie
	}

	if *epochsFlag == "" || *committeesFlag == "" {
		fmt.Fprintln(os.Stderr, "--epochs and --committees are required")
		os.Exit(1)
	}

	committees, err := ParseCommittees(*committeesFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid committees: %v\n", err)
		os.Exit(1)
	}

	useAverage := *averageFlag || *avgFlag

	if useAverage && strings.Contains(*epochsFlag, ";") {
		// Multi-period mode
		if len(committees) != 1 {
			fmt.Fprintln(os.Stderr, "Multi-period average mode only supports one committee group at a time.")
			os.Exit(1)
		}
		periods, err := ParseEpochPeriods(*epochsFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid epochs: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Periods: %v\n", *epochsFlag)
		fmt.Printf("Committee: %s\n", CommitteeGroupToIntervalString(committees[0]))
		fmt.Println("Base URL:", *baseURLFlag)

		var periodLabels []string
		var effs, corrs []float64
		for _, period := range periods {
			totalEff, totalCorr, totalEpochs := 0.0, 0.0, 0
			labelParts := []string{}
			for _, r := range period {
				from, to := r[0], r[1]
				labelParts = append(labelParts, fmt.Sprintf("%d-%d", from, to))
				resp, epochs, err := FetchStatsRange(*baseURLFlag, FlattenGroup(committees[0]), from, to, cookie)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error fetching stats for range %d-%d: %v\n", from, to, err)
					continue
				}
				totalEff += resp.Effectiveness * float64(epochs)
				totalCorr += resp.Correctness * float64(epochs)
				totalEpochs += epochs
			}
			if totalEpochs > 0 {
				effs = append(effs, totalEff/float64(totalEpochs))
				corrs = append(corrs, totalCorr/float64(totalEpochs))
			} else {
				effs = append(effs, 0)
				corrs = append(corrs, 0)
			}
			periodLabels = append(periodLabels, strings.Join(labelParts, ","))
		}
		// Output
		if *csvFlag != "" {
			file, err := os.Create(*csvFlag)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error exporting CSV: %v\n", err)
				os.Exit(1)
			}
			defer file.Close()
			fmt.Fprintf(file, "Period,Effectiveness,Correctness\n")
			for i := range periodLabels {
				fmt.Fprintf(file, "%s,%.4f,%.4f\n", periodLabels[i], effs[i], corrs[i])
			}
			fmt.Printf("Exported to %s\n", *csvFlag)
		} else if *plotFlag != "" {
			// Bar chart: periods on X, two bars per period
			file := *plotFlag
			if file == "" {
				file = "plot.html"
			}
			f, err := os.Create(file)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error exporting plot HTML: %v\n", err)
				os.Exit(1)
			}
			defer f.Close()
			html := GenerateBarChartHTML(periodLabels, effs, corrs, CommitteeGroupToIntervalString(committees[0]))
			_, err = f.WriteString(html)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error writing plot HTML: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Exported plot to %s\n", file)
		} else {
			fmt.Println()
			fmt.Printf("%-20s | %-15s | %-15s\n", "Period", "Effectiveness", "Correctness")
			fmt.Println(strings.Repeat("-", 55))
			for i := range periodLabels {
				fmt.Printf("%-20s | %-15.4f | %-15.4f\n", periodLabels[i], effs[i], corrs[i])
			}
		}
		return
	}

	epochs, err := ParseEpochs(*epochsFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid epochs: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Epochs:", epochs)
	for i, group := range committees {
		fmt.Printf("Group %d: %s\n", i+1, CommitteeGroupToIntervalString(group))
	}
	fmt.Println("Base URL:", *baseURLFlag)

	var table []TableRow
	for _, epoch := range epochs {
		row := TableRow{Epoch: epoch}
		for _, group := range committees {
			ids := FlattenGroup(group)
			stats, err := FetchStats(*baseURLFlag, ids, epoch, cookie)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error fetching stats for epoch %d, group %s: %v\n", epoch, CommitteeGroupToIntervalString(group), err)
				row.GroupStats = append(row.GroupStats, APIResponse{})
				continue
			}
			row.GroupStats = append(row.GroupStats, *stats)
		}
		table = append(table, row)
	}

	if *plotFlag != "" || (len(*plotFlag) == 0 && os.Getenv("PLOT_HTML") != "") {
		plotFile := *plotFlag
		if plotFile == "" {
			plotFile = "plot.html"
		}
		err := ExportPlotHTML(table, committees, plotFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error exporting plot HTML: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Exported plot to %s\n", plotFile)
		return
	}

	if *csvFlag != "" {
		err := ExportTableCSV(table, committees, *csvFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error exporting CSV: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Exported to %s\n", *csvFlag)
	} else {
		PrintTable(table, committees)
	}
}
