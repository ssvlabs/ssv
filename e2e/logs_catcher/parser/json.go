package parser

import "encoding/json"

func JSON(log string) (map[string]string, error) {
	logmap := make(map[string]string)
	err := json.Unmarshal([]byte(log), &logmap)
	if err != nil {
		return nil, err
	}
	return logmap, nil
}
