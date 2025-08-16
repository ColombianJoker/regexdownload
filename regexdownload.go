package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// findConfigurationFile searches for the configuration file in a specific order.
// It returns the path to the file or an empty string if not found.
func findConfigurationFile() (string, error) {
	executableName, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not get executable name: %w", err)
	}

	baseName := filepath.Base(executableName)
	envVarName := strings.ToUpper(baseName) + "_CONFIG"

	// 1. Check for the environment variable
	if configPath := os.Getenv(envVarName); configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}
	}

	// 2. Search in the current directory
	configFileName := "." + baseName + ".conf"
	if _, err := os.Stat(configFileName); err == nil {
		return configFileName, nil
	}

	// 3. Search in /opt/local/etc
	configPath := filepath.Join("/opt/local/etc", baseName+".conf")
	if _, err := os.Stat(configPath); err == nil {
		return configPath, nil
	}

	// 4. Search in /etc
	configPath = filepath.Join("/etc", baseName+".conf")
	if _, err := os.Stat(configPath); err == nil {
		return configPath, nil
	}

	return "", nil // Not found
}

func main() {
	// Define and parse the verbose flag
	verbose := flag.Bool("v", false, "Enable verbose output")
	flag.BoolVar(verbose, "verbose", false, "Enable verbose output")
	flag.Parse()

	configFile, err := findConfigurationFile()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if configFile == "" {
		fmt.Fprintln(os.Stderr, "Error: Configuration file not found.")
		os.Exit(1)
	}

	if *verbose {
		fmt.Printf("Using configuration file: %s\n", configFile)
	}

	// The rest of the program logic will go here
	fmt.Println("Program continues...")
}
