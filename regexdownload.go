package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/ini.v1"
)

// ProcessResult holds the outcome of a single URL processing goroutine.
type ProcessResult struct {
	URL            string
	FinalPrefix    string
	Err            error
	OutputMessages []string
}

// findConfigurationFile remains the same as before.
func findConfigurationFile() (string, error) {
	executableName, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not get executable name: %w", err)
	}
	baseName := filepath.Base(executableName)
	envVarName := strings.ToUpper(baseName) + "_CONFIG"
	if configPath := os.Getenv(envVarName); configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}
	}
	configFileName := "." + baseName + ".conf"
	if _, err := os.Stat(configFileName); err == nil {
		return configFileName, nil
	}
	configPath := filepath.Join("/opt/local/etc", baseName+".conf")
	if _, err := os.Stat(configPath); err == nil {
		return configPath, nil
	}
	configPath = filepath.Join("/etc", baseName+".conf")
	if _, err := os.Stat(configPath); err == nil {
		return configPath, nil
	}
	return "", nil
}

// processURL is the worker function that handles a single URL.
// It's designed to be run in a goroutine.
func processURL(arg string, cfg *ini.File, wg *sync.WaitGroup, results chan<- ProcessResult) {
	defer wg.Done()

	res := ProcessResult{URL: arg}

	// 1. Parse URL and find the configuration section
	parsedURL, err := url.Parse(arg)
	if err != nil {
		res.Err = fmt.Errorf("could not parse as a URL: %w", err)
		results <- res
		return
	}

	hostname := parsedURL.Hostname()
	parts := strings.Split(hostname, ".")
	if len(parts) < 2 {
		res.Err = fmt.Errorf("hostname '%s' is not a valid domain", hostname)
		results <- res
		return
	}
	sectionName := parts[len(parts)-2]
	res.OutputMessages = append(res.OutputMessages, fmt.Sprintf("Processing section '%s'...", sectionName))

	section, err := cfg.GetSection(sectionName)
	if err != nil {
		res.Err = fmt.Errorf("section '[%s]' not found in config", sectionName)
		results <- res
		return
	}

	// 2. Determine the initial prefix
	hasPrefixRegex := section.HasKey("prefix")
	var prefixValue string
	if hasPrefixRegex {
		prefixValue = section.Key("prefix").String()
	} else {
		prefixValue = fmt.Sprintf("%s-%d", sectionName, time.Now().Unix())
	}
	res.FinalPrefix = prefixValue // Set initial prefix

	// 3. Download the URL content to a temporary file
	if !strings.HasPrefix(arg, "http://") && !strings.HasPrefix(arg, "https://") {
		res.OutputMessages = append(res.OutputMessages, "Argument is not a downloadable HTTP/S URL.")
		results <- res
		return
	}

	resp, err := http.Get(arg)
	if err != nil {
		res.Err = fmt.Errorf("failed to download: %w", err)
		results <- res
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		res.Err = fmt.Errorf("download failed with status: %s", resp.Status)
		results <- res
		return
	}

	tmpFile, err := os.CreateTemp("", "regexdownload-*.tmp")
	if err != nil {
		res.Err = fmt.Errorf("failed to create temp file: %w", err)
		results <- res
		return
	}
	defer os.Remove(tmpFile.Name()) // Ensure cleanup

	_, err = io.Copy(tmpFile, resp.Body)
	tmpFile.Close() // Close the file so we can read it
	if err != nil {
		res.Err = fmt.Errorf("failed to write to temp file: %w", err)
		results <- res
		return
	}

	// 4. If a prefix regex was provided, process the file content
	if hasPrefixRegex {
		content, err := os.ReadFile(tmpFile.Name())
		if err != nil {
			res.Err = fmt.Errorf("failed to read temp file: %w", err)
			results <- res
			return
		}

		re, err := regexp.Compile(prefixValue)
		if err != nil {
			res.Err = fmt.Errorf("invalid prefix regex '%s': %w", prefixValue, err)
			results <- res
			return
		}

		matches := re.FindSubmatch(content)
		if len(matches) >= 2 { // A match was found with at least one capture group
			res.FinalPrefix = string(matches[1]) // Update the prefix with the first capture
			res.OutputMessages = append(res.OutputMessages, "  Prefix extracted from content.")
		} else {
			res.OutputMessages = append(res.OutputMessages, "  Prefix regex did not match or had no capture group.")
		}
	}

	results <- res
}

func main() {
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

	cfg, err := ini.Load(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to read configuration file '%s': %v\n", configFile, err)
		os.Exit(1)
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: No URL arguments provided.")
		os.Exit(1)
	}

	var wg sync.WaitGroup
	results := make(chan ProcessResult, len(args))

	for _, arg := range args {
		wg.Add(1)
		go processURL(arg, cfg, &wg, results)
	}

	// Wait for all goroutines to finish, then close the channel
	wg.Wait()
	close(results)

	// Process all results from the channel
	for res := range results {
		fmt.Printf("--- Result for %s ---\n", res.URL)
		if res.Err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", res.Err)
		}
		for _, msg := range res.OutputMessages {
			fmt.Println(msg)
		}
		if *verbose && res.FinalPrefix != "" {
			fmt.Printf("  Final Prefix: %s\n", res.FinalPrefix)
		}
	}
}
