package main

import (
	"encoding/json"
	"fmt"
	"os"
)

var (
	configFile string = getEnv("CONFIG_FILE", "config.json")
)

func readConfig() (*Config, error) {
	// Get endpoint list
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("error reading endpoint file:%s", err)
	}

	// Parse JSON data
	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("error parsing JSON:%s", err)
	}

	return &config, nil
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
