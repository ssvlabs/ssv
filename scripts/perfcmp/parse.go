package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ParseEpochs parses epoch input like "1", "1-4", "1-2,4-5" into a sorted slice of ints
func ParseEpochs(input string) ([]int, error) {
	var epochs []int
	parts := strings.Split(input, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid epoch range: %s", part)
			}
			start, err1 := strconv.Atoi(rangeParts[0])
			end, err2 := strconv.Atoi(rangeParts[1])
			if err1 != nil || err2 != nil || start > end {
				return nil, fmt.Errorf("invalid epoch range: %s", part)
			}
			for i := start; i <= end; i++ {
				epochs = append(epochs, i)
			}
		} else {
			epoch, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid epoch: %s", part)
			}
			epochs = append(epochs, epoch)
		}
	}
	// Remove duplicates and sort
	epochMap := make(map[int]struct{})
	for _, e := range epochs {
		epochMap[e] = struct{}{}
	}
	unique := make([]int, 0, len(epochMap))
	for e := range epochMap {
		unique = append(unique, e)
	}
	sort.Ints(unique)
	return unique, nil
}

// ParseCommittees parses committee input like "1,2,3,4;5,6,7,8+9,10,11,12" or "1-4;5-12"
// Returns a slice of groups, each group is a slice of slices (for +), each inner slice is a list of ints
func ParseCommittees(input string) ([][][]int, error) {
	groups := strings.Split(input, ";")
	var result [][][]int
	for _, group := range groups {
		combos := strings.Split(group, "+")
		var comboList [][]int
		for _, combo := range combos {
			combo = strings.TrimSpace(combo)
			var ids []int
			parts := strings.Split(combo, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if strings.Contains(part, "-") {
					rangeParts := strings.SplitN(part, "-", 2)
					if len(rangeParts) != 2 {
						return nil, fmt.Errorf("invalid committee range: %s", part)
					}
					start, err1 := strconv.Atoi(rangeParts[0])
					end, err2 := strconv.Atoi(rangeParts[1])
					if err1 != nil || err2 != nil || start > end {
						return nil, fmt.Errorf("invalid committee range: %s", part)
					}
					for i := start; i <= end; i++ {
						ids = append(ids, i)
					}
				} else {
					id, err := strconv.Atoi(part)
					if err != nil {
						return nil, fmt.Errorf("invalid committee id: %s", part)
					}
					ids = append(ids, id)
				}
			}
			comboList = append(comboList, ids)
		}
		result = append(result, comboList)
	}
	return result, nil
}

// CommitteeGroupToIntervalString converts a group ([][]int) to interval notation string, e.g. "1-4,9-12"
func CommitteeGroupToIntervalString(group [][]int) string {
	var all []int
	for _, combo := range group {
		all = append(all, combo...)
	}
	if len(all) == 0 {
		return ""
	}
	sort.Ints(all)
	// Merge into intervals
	var intervals []string
	start := all[0]
	prev := all[0]
	for i := 1; i < len(all); i++ {
		if all[i] == prev+1 {
			prev = all[i]
			continue
		}
		if start == prev {
			intervals = append(intervals, fmt.Sprintf("%d", start))
		} else {
			intervals = append(intervals, fmt.Sprintf("%d-%d", start, prev))
		}
		start = all[i]
		prev = all[i]
	}
	if start == prev {
		intervals = append(intervals, fmt.Sprintf("%d", start))
	} else {
		intervals = append(intervals, fmt.Sprintf("%d-%d", start, prev))
	}
	return strings.Join(intervals, ",")
}

// FlattenGroup returns all committee IDs in a group ([][]int)
func FlattenGroup(group [][]int) []int {
	var all []int
	for _, combo := range group {
		all = append(all, combo...)
	}
	return all
}
