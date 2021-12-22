package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"text/template"
)

// Usage:
// ./tmpl-cli <template file> <template data (json)>
//
// example:
// ./tmpl-cli "$PWD/resources/docker-compose.yaml" "[1, 2, 3, 4, 5]"

func main() {
	args := os.Args[1:]

	// read template file
	tmplRaw, err := ioutil.ReadFile(args[0])
	if err != nil {
		panic(err)
	}
	// read template data as json
	var data interface{}
	if err := json.Unmarshal([]byte(args[1]), &data); err != nil {
		panic(err)
	}

	t := template.Must(template.New("tmpl").Parse(string(tmplRaw)))

	if err := t.Execute(os.Stdout, data); err != nil {
		panic(err)
	}
}
