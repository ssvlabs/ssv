package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
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
		averageFlag    = flag.Bool("average", false, "If set, fetch and print/export only the average for the range (requires --epochs to be a single range or multi-period)")
		avgFlag        = flag.Bool("avg", false, "Alias for --average")
		csvFlag        = flag.String("csv", "", "CSV output file (optional, if not set, prints to stdout)")
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

	committeeGroups := ParseCommitteeGroups(*committeesFlag)

	useAverage := *averageFlag || *avgFlag

	if useAverage {
		// Support multiple ranges: split on ';'
		rangeStrs := strings.Split(*epochsFlag, ";")
		var ranges [][2]int
		var rangeLabels []string
		for _, r := range rangeStrs {
			r = strings.TrimSpace(r)
			if r == "" {
				continue
			}
			parts := strings.Split(r, "-")
			if len(parts) != 2 {
				fmt.Fprintln(os.Stderr, "--average/--avg requires each range to be N-M, e.g. 28370-28390")
				os.Exit(1)
			}
			fromEpoch, err1 := strconv.Atoi(parts[0])
			toEpoch, err2 := strconv.Atoi(parts[1])
			if err1 != nil || err2 != nil || fromEpoch > toEpoch {
				fmt.Fprintf(os.Stderr, "invalid epoch range: %s\n", r)
				os.Exit(1)
			}
			ranges = append(ranges, [2]int{fromEpoch, toEpoch})
			rangeLabels = append(rangeLabels, r)
		}
		if len(ranges) == 0 {
			fmt.Fprintln(os.Stderr, "no valid epoch ranges provided")
			os.Exit(1)
		}

		// For each range, for each group, fetch average
		results := make([][]APIResponse, len(ranges))
		for i, rg := range ranges {
			avgMap, groupKeys, err := FetchAverageGroups(*baseURLFlag, committeeGroups, rg[0], rg[1], cookie)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error fetching averages for range %s: %v\n", rangeLabels[i], err)
				os.Exit(1)
			}
			row := make([]APIResponse, len(groupKeys))
			for j, group := range groupKeys {
				row[j] = avgMap[group]
			}
			results[i] = row
		}

		if *csvFlag != "" {
			file, err := os.Create(*csvFlag)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error exporting CSV: %v\n", err)
				os.Exit(1)
			}
			defer file.Close()
			headers := append([]string{"Range"}, committeeGroups...)
			fmt.Fprintln(file, strings.Join(headers, ","))
			for i, row := range results {
				fields := []string{rangeLabels[i]}
				for _, stats := range row {
					fields = append(fields,
						fmt.Sprintf("%.4f", stats.Effectiveness)+","+fmt.Sprintf("%.4f", stats.Correctness),
					)
				}
				fmt.Fprintln(file, strings.Join(fields, ","))
			}
			fmt.Printf("Exported to %s\n", *csvFlag)
			return
		}
		if *plotFlag != "" {
			// For average, plot as grouped bar chart: x=range, bars=groups
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
			// Prepare data for bar chart
			// Each group: slice of values (one per range)
			groupVals := make([][]float64, len(committeeGroups))
			groupCorrs := make([][]float64, len(committeeGroups))
			for j := range committeeGroups {
				groupVals[j] = make([]float64, len(ranges))
				groupCorrs[j] = make([]float64, len(ranges))
				for i := range ranges {
					groupVals[j][i] = results[i][j].Effectiveness
					groupCorrs[j][i] = results[i][j].Correctness
				}
			}
			html := GenerateBarChartHTMLMulti(rangeLabels, committeeGroups, groupVals, groupCorrs)
			_, err = f.WriteString(html)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error writing plot HTML: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Exported plot to %s\n", file)
			return
		}

		// Print table
		fmt.Println()
		headers := append([]string{"Range"}, committeeGroups...)
		fmt.Println(strings.Join(headers, " | "))
		fmt.Println(strings.Repeat("-", 16*len(headers)))
		for i, row := range results {
			fields := []string{rangeLabels[i]}
			for _, stats := range row {
				fields = append(fields,
					fmt.Sprintf("%.4f", stats.Effectiveness)+"/"+fmt.Sprintf("%.4f", stats.Correctness),
				)
			}
			fmt.Println(strings.Join(fields, " | "))
		}
		return
	}

	// Non-average: support multiple ranges
	rangeStrs := strings.Split(*epochsFlag, ";")
	epochSet := make(map[int]struct{})
	var allRanges [][2]int
	for _, r := range rangeStrs {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		parts := strings.Split(r, "-")
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "invalid epoch range: %s\n", r)
			os.Exit(1)
		}
		fromEpoch, err1 := strconv.Atoi(parts[0])
		toEpoch, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || fromEpoch > toEpoch {
			fmt.Fprintf(os.Stderr, "invalid epoch range: %s\n", r)
			os.Exit(1)
		}
		allRanges = append(allRanges, [2]int{fromEpoch, toEpoch})
		for e := fromEpoch; e <= toEpoch; e++ {
			epochSet[e] = struct{}{}
		}
	}
	if len(allRanges) == 0 {
		fmt.Fprintln(os.Stderr, "no valid epoch ranges provided")
		os.Exit(1)
	}
	// Collect all epochs, sorted
	epochs := make([]int, 0, len(epochSet))
	for e := range epochSet {
		epochs = append(epochs, e)
	}
	sort.Ints(epochs)

	// For each group, for each range, fetch per-epoch stats
	// Merge all results into map[group][epoch]APIResponse
	groupEpochStats := make(map[string]map[int]APIResponse)
	for _, group := range committeeGroups {
		groupEpochStats[group] = make(map[int]APIResponse)
		for _, rg := range allRanges {
			batch, _, err := FetchEpochsBatchGroups(*baseURLFlag, []string{group}, rg[0], rg[1], cookie)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error fetching stats for group %s, range %d-%d: %v\n", group, rg[0], rg[1], err)
				os.Exit(1)
			}
			for epoch, stats := range batch[group] {
				groupEpochStats[group][epoch] = stats
			}
		}
	}

	// Build table
	table := make([]TableRow, 0, len(epochSet))
	for _, epoch := range epochs {
		row := TableRow{Epoch: epoch}
		for _, group := range committeeGroups {
			stats, ok := groupEpochStats[group][epoch]
			if !ok {
				row.GroupStats = append(row.GroupStats, APIResponse{})
			} else {
				row.GroupStats = append(row.GroupStats, stats)
			}
		}
		table = append(table, row)
	}

	if *csvFlag != "" {
		file, err := os.Create(*csvFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error exporting CSV: %v\n", err)
			os.Exit(1)
		}
		defer file.Close()
		headers := append([]string{"Epoch"}, committeeGroups...)
		fmt.Fprintln(file, strings.Join(headers, ","))
		for _, row := range table {
			fields := []string{strconv.Itoa(row.Epoch)}
			for _, stats := range row.GroupStats {
				fields = append(fields,
					fmt.Sprintf("%.4f", stats.Effectiveness)+","+fmt.Sprintf("%.4f", stats.Correctness),
				)
			}
			fmt.Fprintln(file, strings.Join(fields, ","))
		}
		fmt.Printf("Exported to %s\n", *csvFlag)
		return
	}
	if *plotFlag != "" {
		file := *plotFlag
		if file == "" {
			file = "plot.html"
		}
		err := ExportPlotHTML(table, committeeGroups, file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error exporting plot HTML: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Exported plot to %s\n", file)
		return
	}

	// Print table with group strings as headers
	headers := append([]string{"Epoch"}, committeeGroups...)
	fmt.Println(strings.Join(headers, " | "))
	fmt.Println(strings.Repeat("-", 16*len(headers)))
	for _, row := range table {
		fields := []string{strconv.Itoa(row.Epoch)}
		for _, stats := range row.GroupStats {
			fields = append(fields,
				fmt.Sprintf("%.4f", stats.Effectiveness)+"/"+fmt.Sprintf("%.4f", stats.Correctness),
			)
		}
		fmt.Println(strings.Join(fields, " | "))
	}
}
