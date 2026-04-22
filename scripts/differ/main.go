package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/aquasecurity/table"
	"github.com/cespare/xxhash/v2"
	"gopkg.in/yaml.v3"
)

type Comparison struct {
	Packages struct {
		Left  []string `yaml:"Left"`
		Right []string `yaml:"Right"`
	} `yaml:"Packages"`
	Hints map[string]string `yaml:"Hints"`
}

type Config struct {
	// ApprovedChanges is a list of change IDs that are considered approved
	// and should be ignored.
	ApprovedChanges []string `yaml:"ApprovedChanges"`

	// IgnoredIdentifiers is a list of Go identifiers that should be ignored.
	//
	// For example, if "logger" is ignored, then any method call on a variable
	// named "logger" and any parameter named "logger" will be removed.
	IgnoredIdentifiers []string `yaml:"IgnoredIdentifiers"`

	// ReducedPackageNames is a list of package names that should be reduced
	// to their last component.
	//
	// For example, if "ssv" is given, then any reference to "ssv.Message"
	// will be reduced to just "Message".
	ReducedPackageNames []string `yaml:"ReducedPackageNames"`

	// Comparisons is a list of pairs of packages to compare.
	Comparisons []Comparison `yaml:"Comparisons"`
}

var cli struct {
	Config string `default:"config.yaml" help:"Path to the config file." type:"existingfile"`
	Left   string `arg:"" help:"Path to the left codebase." type:"existingdir"`
	Right  string `arg:"" help:"Path to the right codebase." type:"existingdir"`
	Output string `default:"output.diff" help:"Path to the output diff file." type:"path"`
}

func run() (changes int, err error) {
	// Parse the config.yaml file.
	var config Config
	yamlFile, err := ioutil.ReadFile(cli.Config)
	if err != nil {
		return 0, fmt.Errorf("failed to read config file: %w", err)
	}
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		return 0, fmt.Errorf("failed to unmarshal YAML: %w", err)
	}

	// Prepare the transformers.
	transformers := Transformers{
		NoComments(),
		NoPackageNames(config.ReducedPackageNames),
		NoEmptyLines(),
	}

	// Allocate a map of approved changes for fast access.
	approvedChanges := map[string]struct{}{}
	for _, change := range config.ApprovedChanges {
		approvedChanges[change] = struct{}{}
	}

	// Create the diff file.
	diffFile, err := os.Create(cli.Output)
	if err != nil {
		return 0, fmt.Errorf("failed to create diff file: %w", err)
	}
	defer diffFile.Close()

	// Compare each pair of packages.
	tbl := table.New(os.Stdout)
	tbl.SetHeaders(filepath.Base(cli.Left), filepath.Base(cli.Right), "Symbol", "Changed")
	var total, changed int
	for _, pair := range config.Comparisons {
		lefts, err := NewParser(cli.Left, pair.Packages.Left, config.IgnoredIdentifiers).Parse()
		if err != nil {
			return 0, fmt.Errorf("failed to parse left package: %w", err)
		}
		rights, err := NewParser(cli.Right, pair.Packages.Right, config.IgnoredIdentifiers).Parse()
		if err != nil {
			return 0, fmt.Errorf("failed to parse right package: %w", err)
		}

		// For each element in the left package, find the corresponding element
		// in the right package and compare.
		sortedNames := slices.Collect(maps.Keys(lefts))
		sort.Strings(sortedNames)
		for _, name := range sortedNames {
			left := lefts[name]

			rightName := name
			if hint, ok := pair.Hints[name]; ok {
				rightName = hint
			}
			right, ok := rights[rightName]
			if !ok {
				continue
			}

			// Apply transformations to the code before comparing.
			for _, e := range []*Element{&left, &right} {
				e.Code = transformers.Transform(e.Code)
			}

			// Compute a unique ID of the change.
			parts := strings.Join(
				[]string{left.Path, right.Path, name, left.Code, right.Code},
				"-- 🦄 muEe9BlmClrXa6g3 🦄 --", // Magic separator.
			)
			diffID := fmt.Sprintf("%x", xxhash.Sum64String(parts))
			_, approved := approvedChanges[diffID]

			// If the change is not approved, append it to the diff file.
			if !approved {
				leftName := fmt.Sprintf("a/%s@%s", left.Path, left.Name)
				rightName := fmt.Sprintf("b/%s@%s", right.Path, right.Name)
				diff, err := Diff(leftName, rightName, []byte(left.Code), []byte(right.Code), 100)
				if err != nil {
					return 0, fmt.Errorf("failed to generate diff: %w", err)
				}
				if len(diff) == 0 {
					// No changes.
					approved = true
				} else {
					fmt.Fprintf(diffFile, "diff --git %s %s\nindex %s..2222222\n%s",
						leftName, rightName, diffID, diff)
				}
			}

			icon := "✅"
			if !approved {
				changed++
				icon = "❌"
			}
			total++
			tbl.AddRow(left.Path, right.Path, name, icon)
		}
	}
	tbl.SetFooters("", "", "", fmt.Sprintf("%d/%d", total-changed, total))
	tbl.Render()

	return changed, nil
}

func main() {
	kong.Parse(&cli)

	changed, err := run()
	if err != nil {
		log.Fatalf("an error occurred: %v", err)
	}

	if changed > 0 {
		fmt.Printf("\n✏️  %d differences found. See %s for details.\n\n", changed, cli.Output)
		os.Exit(1)
	}

	fmt.Printf("\n✅ Codebases are identical.\n")
}
