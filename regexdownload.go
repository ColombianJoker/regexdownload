package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/ini.v1"
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

// processArguments loads the config file and processes each command-line URL argument.
func processArguments(configFile string, args []string, verbose bool) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: No URL arguments provided to process.")
		return
	}

	// Load the INI file
	cfg, err := ini.Load(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to read configuration file '%s': %v\n", configFile, err)
		os.Exit(1)
	}

	// Process each argument as a URL
	for _, arg := range args {
		parsedURL, err := url.Parse(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Could not parse argument '%s' as a URL: %v\n", arg, err)
			continue
		}

		hostname := parsedURL.Hostname()
		if hostname == "" {
			fmt.Fprintf(os.Stderr, "Error: Could not extract hostname from URL '%s'.\n", arg)
			continue
		}

		parts := strings.Split(hostname, ".")
		if len(parts) < 2 {
			fmt.Fprintf(os.Stderr, "Error: Hostname '%s' from URL '%s' is not a valid domain.\n", hostname, arg)
			continue
		}

		sectionName := parts[len(parts)-2]
		fmt.Printf("Processing section '%s' from URL '%s'...\n", sectionName, arg)

		section, err := cfg.GetSection(sectionName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Section '[%s]' not found in the configuration file.\n", sectionName)
			continue
		}

		// --- New Prefix Logic ---
		var prefixValue string
		if section.HasKey("prefix") {
			// Use the prefix from the config file
			prefixValue = section.Key("prefix").String()
		} else {
			// Generate a prefix using section name and timestamp
			timestamp := time.Now().Unix()
			prefixValue = fmt.Sprintf("%s-%d", sectionName, timestamp)
		}

		if verbose {
			fmt.Printf("  Prefix: %s\n", prefixValue)
		}
		// --- End of New Logic ---

		// Print all key-value pairs in the section, skipping the special 'prefix' key
		for _, key := range section.Keys() {
			if key.Name() == "prefix" {
				continue // Don't treat the prefix as a regular expression
			}
			fmt.Printf("%s = %s\n", key.Name(), key.String())
		}
		fmt.Println("---")
	}
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

	args := flag.Args()
	// Pass the verbose flag to the processing function
	processArguments(configFile, args, *verbose)
}
