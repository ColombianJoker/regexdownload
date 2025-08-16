package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

// processArguments loads the config file and processes each command-line argument.
func processArguments(configFile string, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: No arguments provided to process.")
		return
	}

	// Compile the regular expression to match 'ARGUMENT.domain'
	// It ensures the domain part does not contain dots.
	re := regexp.MustCompile(`^([^.]+)\.[^.]+$`)

	// Load the INI file
	cfg, err := ini.Load(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to read configuration file '%s': %v\n", configFile, err)
		os.Exit(1)
	}

	// Process each argument
	for _, arg := range args {
		matches := re.FindStringSubmatch(arg)
		if matches == nil || len(matches) < 2 {
			fmt.Fprintf(os.Stderr, "Error: Argument '%s' does not match the required 'ARGUMENT.domain' format.\n", arg)
			continue
		}

		sectionName := matches[1] // The first capture group is our section name
		fmt.Printf("Processing section '%s' from argument '%s'...\n", sectionName, arg)

		section, err := cfg.GetSection(sectionName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Section '[%s]' not found in the configuration file.\n", sectionName)
			continue
		}

		// Print all key-value pairs in the section
		for _, key := range section.Keys() {
			fmt.Printf("%s = %s\n", key.Name(), key.String())
		}
		fmt.Println("---") // Separator for clarity
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

	// Get the non-flag arguments from the command line
	args := flag.Args()
	processArguments(configFile, args)
}
