package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// APIResponse models the dashboard API response
// Only relevant fields for now
// Example: {"Effectiveness":0.95,"Correctness":0.96,...}
type APIResponse struct {
	Effectiveness float64 `json:"Effectiveness"`
	Correctness   float64 `json:"Correctness"`
}

// FetchStats fetches stats for a given epoch and committee group, with optional cookie
func FetchStats(baseURL string, committees []int, epoch int, cookie string) (*APIResponse, error) {
	committeesStrs := make([]string, len(committees))
	for i, c := range committees {
		committeesStrs[i] = strconv.Itoa(c)
	}
	url := fmt.Sprintf("%s?committees=%s&epoch=%d", baseURL, strings.Join(committeesStrs, ","), epoch)

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http status: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	var parsed struct {
		Data APIResponse `json:"Data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}
	return &parsed.Data, nil
}

// FetchStatsRange fetches stats for a committee group over a range using from/to
// Returns APIResponse, number of epochs in range, error
func FetchStatsRange(baseURL string, committees []int, from, to int, cookie string) (APIResponse, int, error) {
	committeesStrs := make([]string, len(committees))
	for i, c := range committees {
		committeesStrs[i] = strconv.Itoa(c)
	}
	url := fmt.Sprintf("%s?committees=%s&from=%d&to=%d", baseURL, strings.Join(committeesStrs, ","), from, to)

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return APIResponse{}, 0, fmt.Errorf("new request: %w", err)
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := client.Do(req)
	if err != nil {
		return APIResponse{}, 0, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return APIResponse{}, 0, fmt.Errorf("http status: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return APIResponse{}, 0, fmt.Errorf("read body: %w", err)
	}
	var parsed struct {
		Data APIResponse `json:"Data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return APIResponse{}, 0, fmt.Errorf("json: %w", err)
	}
	return parsed.Data, to - from + 1, nil
}

// FetchEpochsBatch fetches per-epoch stats for one or more committee groups over a contiguous range using the /api/stats/epochs endpoint
// groups: slice of committee groups (each group is a slice of ints)
// Returns map[groupKey][epoch]APIResponse, []groupKey, error
func FetchEpochsBatch(baseURL string, groups [][]int, from, to int, cookie string) (map[string]map[int]APIResponse, []string, error) {
	batchURL := baseURL
	if strings.HasSuffix(batchURL, "/stats") {
		batchURL = strings.TrimSuffix(batchURL, "/stats") + "/stats/epochs"
	}
	result := make(map[string]map[int]APIResponse)
	groupKeys := make([]string, len(groups))
	for i, group := range groups {
		ids := make([]string, len(group))
		for j, id := range group {
			ids[j] = strconv.Itoa(id)
		}
		groupKey := strings.Join(ids, ",")
		groupKeys[i] = groupKey
		url := fmt.Sprintf("%s?committees=%s&from=%d&to=%d", batchURL, groupKey, from, to)
		client := &http.Client{}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("new request: %w", err)
		}
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, nil, fmt.Errorf("http get: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, nil, fmt.Errorf("http status: %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("read body: %w", err)
		}
		var parsed struct {
			Data map[string]APIResponse `json:"Data"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, nil, fmt.Errorf("json: %w", err)
		}
		result[groupKey] = make(map[int]APIResponse)
		for epochStr, v := range parsed.Data {
			epoch, err := strconv.Atoi(epochStr)
			if err != nil {
				continue
			}
			result[groupKey][epoch] = v
		}
	}
	return result, groupKeys, nil
}

// FetchEpochsBatchGroups fetches per-epoch stats for each group string in groups (as-is)
// Returns map[group][epoch]APIResponse, []group, error
func FetchEpochsBatchGroups(baseURL string, groups []string, from, to int, cookie string) (map[string]map[int]APIResponse, []string, error) {
	batchURL := baseURL
	if strings.HasSuffix(batchURL, "/stats") {
		batchURL = strings.TrimSuffix(batchURL, "/stats") + "/stats/epochs"
	}
	result := make(map[string]map[int]APIResponse)
	for _, group := range groups {
		url := fmt.Sprintf("%s?committees=%s&from=%d&to=%d", batchURL, group, from, to)
		client := &http.Client{}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("new request: %w", err)
		}
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, nil, fmt.Errorf("http get: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, nil, fmt.Errorf("http status: %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("read body: %w", err)
		}
		var parsed struct {
			Data map[string]APIResponse `json:"Data"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, nil, fmt.Errorf("json: %w", err)
		}
		result[group] = make(map[int]APIResponse)
		for epochStr, v := range parsed.Data {
			epoch, err := strconv.Atoi(epochStr)
			if err != nil {
				continue
			}
			result[group][epoch] = v
		}
	}
	return result, groups, nil
}

// FetchAverageGroups fetches average stats for each group string in groups (as-is)
// Returns map[group]APIResponse, []group, error
func FetchAverageGroups(baseURL string, groups []string, from, to int, cookie string) (map[string]APIResponse, []string, error) {
	result := make(map[string]APIResponse)
	for _, group := range groups {
		url := fmt.Sprintf("%s?committees=%s&from=%d&to=%d", baseURL, group, from, to)
		client := &http.Client{}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("new request: %w", err)
		}
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, nil, fmt.Errorf("http get: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, nil, fmt.Errorf("http status: %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("read body: %w", err)
		}
		var parsed struct {
			Data APIResponse `json:"Data"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, nil, fmt.Errorf("json: %w", err)
		}
		result[group] = parsed.Data
	}
	return result, groups, nil
}
