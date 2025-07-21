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
