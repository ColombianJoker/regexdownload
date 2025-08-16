package main

import (
	"flag"
	"fmt"
	"html"
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
	FoundURLs      []string // To hold the matched image URLs
	Err            error
	OutputMessages []string
}

// cleanPrefix sanitizes a string to make it suitable for use in a filename.
func cleanPrefix(s string) string {
	cleaned := html.UnescapeString(s)
	slashReplacer := strings.NewReplacer("/", "", "\\", "")
	cleaned = slashReplacer.Replace(cleaned)
	reWhitespace := regexp.MustCompile("[ \t\u00A0]+")
	cleaned = reWhitespace.ReplaceAllString(cleaned, " ")
	cleaned = strings.TrimSpace(cleaned)
	return cleaned
}

// findConfigurationFile searches for the configuration file in a specific order.
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

	// 4. Search in /etc - ** THIS BLOCK IS NOW CORRECT **
	configPath = filepath.Join("/etc", baseName+".conf") // Use assignment "=" instead of ":="
	if _, err := os.Stat(configPath); err == nil {
		return configPath, nil
	}

	return "", nil // Not found
}

// processURL is the worker function that handles a single URL.
func processURL(arg string, cfg *ini.File, wg *sync.WaitGroup, results chan<- ProcessResult) {
	defer wg.Done()
	res := ProcessResult{URL: arg}

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

	hasPrefixRegex := section.HasKey("prefix")
	var prefixValue string
	if hasPrefixRegex {
		prefixValue = section.Key("prefix").String()
	} else {
		prefixValue = fmt.Sprintf("%s-%d", sectionName, time.Now().Unix())
	}
	res.FinalPrefix = prefixValue

	if !strings.HasPrefix(arg, "http://") && !strings.HasPrefix(arg, "https://") {
		res.OutputMessages = append(res.OutputMessages, "Argument is not a downloadable HTTP/S URL.")
		res.FinalPrefix = cleanPrefix(res.FinalPrefix)
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
	defer os.Remove(tmpFile.Name())

	_, err = io.Copy(tmpFile, resp.Body)
	tmpFile.Close()
	if err != nil {
		res.Err = fmt.Errorf("failed to write to temp file: %w", err)
		results <- res
		return
	}

	// ** EFFICIENT FILE HANDLING: Read the file only ONCE **
	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		res.Err = fmt.Errorf("failed to read temp file: %w", err)
		results <- res
		return
	}

	// First, process the prefix using the file content
	if hasPrefixRegex {
		re, err := regexp.Compile(prefixValue)
		if err != nil {
			res.Err = fmt.Errorf("invalid prefix regex '%s': %w", prefixValue, err)
			results <- res
			return
		}
		matches := re.FindSubmatch(content)
		if len(matches) >= 2 {
			res.FinalPrefix = string(matches[1]) // Update prefix from capture group
			res.OutputMessages = append(res.OutputMessages, "  Prefix extracted from content.")
		} else {
			res.OutputMessages = append(res.OutputMessages, "  Prefix regex did not match or had no capture group.")
		}
	}

	// Second, find all image URLs using the same file content
	for _, key := range section.Keys() {
		if strings.HasPrefix(key.Name(), "re") {
			regexString := key.String()
			re, err := regexp.Compile(regexString)
			if err != nil {
				// Don't exit, just note the error and continue to the next regex
				res.OutputMessages = append(res.OutputMessages, fmt.Sprintf("  Warning: Invalid regex for key '%s': %v", key.Name(), err))
				continue
			}
			// Find all non-overlapping matches in the content
			matches := re.FindAllSubmatch(content, -1)
			for _, match := range matches {
				if len(match) > 1 { // We need a capture group
					res.FoundURLs = append(res.FoundURLs, string(match[1])) // Add the content of the first capture group
				}
			}
		}
	}

	res.FinalPrefix = cleanPrefix(res.FinalPrefix)
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
		fmt.Fprintf(os.Stderr, "Error: Failed to read config file '%s': %v\n", configFile, err)
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

	wg.Wait()
	close(results)

	for res := range results {
		fmt.Printf("--- Result for %s ---\n", res.URL)
		if res.Err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", res.Err)
		}
		for _, msg := range res.OutputMessages {
			fmt.Println(msg)
		}
		if *verbose && res.FinalPrefix != "" {
			fmt.Printf("  Cleaned Final Prefix: %s\n", res.FinalPrefix)
		}
		if len(res.FoundURLs) > 0 {
			fmt.Println("Found URLs:")
			for _, u := range res.FoundURLs {
				fmt.Printf("%s\n", u)
			}
		} else {
			fmt.Println("No URLs found.")
		}
	}
}
