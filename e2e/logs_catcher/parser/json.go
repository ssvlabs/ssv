package parser

import "encoding/json"

func JSON(log string) (map[string]any, error) {
	logmap := make(map[string]any)
	err := json.Unmarshal([]byte(log), &logmap)
	if err != nil {
		return nil, err
	}
	return logmap, nil
}
