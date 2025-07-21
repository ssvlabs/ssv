package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// To use authentication, create a .env file in scripts/perfcmp/ with:
// VAULT_AUTH_COOKIE=your_cookie_value

func main() {
	var (
		epochsFlag     = flag.String("epochs", "", "Epoch(s) to analyze, e.g. 1,2,3 or 1-4 or 1-2,4-5")
		committeesFlag = flag.String("committees", "", "Committees to compare, e.g. 1,2,3,4;5,6,7,8+9,10,11,12 or 1-4;5-12")
		baseURLFlag    = flag.String("base-url", "https://e2m-hoodi.stage.ops.ssvlabsinternal.com/api/stats", "Base URL for dashboard API")
		cookieFlag     = flag.String("cookie", "", "vaultAuthCookie value (optional, can also be set in .env as VAULT_AUTH_COOKIE)")
		csvFlag        = flag.String("csv", "", "CSV output file (optional, if not set, prints to stdout)")
		averageFlag    = flag.Bool("average", false, "If set, fetch and print/export only the average for the range (requires --epochs to be a single range)")
		avgFlag        = flag.Bool("avg", false, "Alias for --average")
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

	if useAverage {
		// Parse epoch range
		parts := strings.Split(*epochsFlag, "-")
		if len(parts) != 2 {
			fmt.Fprintln(os.Stderr, "--average/--avg requires --epochs to be a single range, e.g. 28370-28390")
			os.Exit(1)
		}
		fromEpoch, err1 := strconv.Atoi(parts[0])
		toEpoch, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || fromEpoch > toEpoch {
			fmt.Fprintln(os.Stderr, "invalid epoch range for --average/--avg")
			os.Exit(1)
		}
		fmt.Printf("Range: %d-%d\n", fromEpoch, toEpoch)
		for i, group := range committees {
			fmt.Printf("Group %d: %s\n", i+1, CommitteeGroupToIntervalString(group))
		}
		fmt.Println("Base URL:", *baseURLFlag)

		var responses []APIResponse
		for _, group := range committees {
			ids := FlattenGroup(group)
			stats, err := FetchStatsRange(*baseURLFlag, ids, fromEpoch, toEpoch, cookie)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error fetching stats for range %d-%d, group %s: %v\n", fromEpoch, toEpoch, CommitteeGroupToIntervalString(group), err)
				responses = append(responses, APIResponse{})
				continue
			}
			responses = append(responses, *stats)
		}
		if *csvFlag != "" {
			err := ExportAverageCSV(responses, committees, fromEpoch, toEpoch, *csvFlag)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error exporting CSV: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Exported to %s\n", *csvFlag)
		} else {
			PrintAverageTable(responses, committees, fromEpoch, toEpoch)
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
