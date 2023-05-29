package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/aquasecurity/table"
	"github.com/aymanbagabas/go-udiff"
	"github.com/aymanbagabas/go-udiff/myers"
	"github.com/cespare/xxhash/v2"
	"github.com/pkg/errors"
	"golang.org/x/exp/maps"
	"gopkg.in/yaml.v3"
)

type Comparison struct {
	Left  []string `yaml:"Left"`
	Right []string `yaml:"Right"`
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
	// For example, if "ssv" is ignored, then any reference to "ssv.Message"
	// will be reduced to "Message".
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
	yamlFile, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		return 0, errors.Wrap(err, "failed to read config file")
	}
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		return 0, errors.Wrap(err, "failed to unmarshal YAML")
	}

	// Prepare the transformers.
	transformers := []Transformer{
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
		return 0, errors.Wrap(err, "failed to create diff file")
	}
	defer diffFile.Close()

	// Compare each pair of packages.
	tbl := table.New(os.Stdout)
	tbl.SetHeaders(filepath.Base(cli.Left), filepath.Base(cli.Right), "Symbol", "Changed")
	var total, changed int
	for _, pair := range config.Comparisons {
		lefts, err := NewParser(cli.Left, pair.Left, config.IgnoredIdentifiers).Parse()
		if err != nil {
			return 0, errors.Wrap(err, "failed to parse left package")
		}
		rights, err := NewParser(cli.Right, pair.Right, config.IgnoredIdentifiers).Parse()
		if err != nil {
			return 0, errors.Wrap(err, "failed to parse right package")
		}

		// For each element in the left package, find the corresponding element
		// in the right package and compare.
		elements := maps.Keys(lefts)
		sort.Strings(elements)
		for _, name := range elements {
			left := lefts[name]
			right, ok := rights[name]
			if !ok {
				continue
			}

			// Apply transformations to the code before comparing.
			for _, e := range []*Element{&left, &right} {
				for _, transformer := range transformers {
					e.Code = transformer(e.Code)
				}
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
				edits := myers.ComputeEdits(left.Code, right.Code)
				udiff.SortEdits(edits)
				if len(edits) == 0 {
					// No changes.
					approved = true
				} else {
					diff, err := udiff.ToUnifiedDiff(leftName, rightName, left.Code, edits)
					if err != nil {
						return 0, errors.Wrap(err, "failed to generate unified diff")
					}
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
