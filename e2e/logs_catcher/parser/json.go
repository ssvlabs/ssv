package parser

import (
	"encoding/json"
	"regexp"
	"strings"
)

func cleanLog(logString string) string {
	// Regular expression to match ANSI color codes
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	cleanedLog := ansiRegex.ReplaceAllString(logString, "")

	// Remove non-JSON characters before the first '{'
	startIndex := strings.Index(cleanedLog, "{")
	if startIndex > -1 {
		return cleanedLog[startIndex:]
	}
	return cleanedLog
}

func JSON(log string) (map[string]any, error) {
	logmap := make(map[string]any)
	err := json.Unmarshal([]byte(cleanLog(log)), &logmap)
	if err != nil {
		return nil, err
	}
	return logmap, nil
}
