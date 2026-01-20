package main

import (
	"dokku-nginx-custom/src/pkg/file_config"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"slices"

	"gopkg.in/yaml.v3"
)

func main() {
	configPath := flag.String("config", "", "Path to YAML config file")
	outputFormat := flag.String("o", "json", "Output format (yaml or json)")
	flag.Parse()

	validOutputFormats := []string{"json", "yaml"}
	if !slices.Contains(validOutputFormats, *outputFormat) {
		log.Fatalf("Invalid output format: %s. Valid formats: %v", *outputFormat, validOutputFormats)
	}

	if *configPath == "" {
		log.Fatal("Please provide a config file path using -config flag")
	}

	// Get query from positional argument
	args := flag.Args()
	var query string
	if len(args) > 0 {
		query = args[0]
	}

	// Read config file
	_, rawConfig, err := file_config.ReadConfig(*configPath)
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}

	// If query is provided, access that specific part of config
	if query != "" {
		result, err := file_config.QueryConfig(rawConfig, query)
		if err != nil {
			log.Fatalf("Error querying config: %v", err)
		}

		switch *outputFormat {
		case "json":
			// If result is a string, print it directly without quotes (like jq -r)
			if str, ok := result.(string); ok {
				fmt.Println(str)
				return
			}

			output, err := json.Marshal(result)
			if err != nil {
				log.Fatalf("Error marshaling query result: %v", err)
			}
			fmt.Println(string(output))
			return
		case "yaml":
			// For YAML, strings are also output without quotes by default
			output, err := yaml.Marshal(result)
			if err != nil {
				log.Fatalf("Error marshaling query result: %v", err)
			}
			fmt.Print(string(output))
			return
		}
	}

	log.Fatalln("Please provide the query as positional argument")
}
