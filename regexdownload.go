package main

import (
	"flag"
	"fmt"

	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path" // Used for extracting file extensions from URLs
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/ini.v1"
)

// ProcessResult holds the outcome of the initial URL parsing.
type ProcessResult struct {
	URL            string
	FinalPrefix    string
	FoundURLs      []string
	Err            error
	OutputMessages []string
}

// DownloadResult holds the outcome of a single file download.
type DownloadResult struct {
	URL      string
	Filepath string
	Err      error
}

// cleanPrefix sanitizes a string for use in a filename.
func cleanPrefix(s string) string {
	cleaned := html.UnescapeString(s)
	slashReplacer := strings.NewReplacer("/", "", "\\", "", "|", "")
	cleaned = slashReplacer.Replace(cleaned)
	// Using double-quotes to correctly handle the \u00A0 unicode character for non-breaking space
	reWhitespace := regexp.MustCompile("[ \t\u00A0]+")
	cleaned = reWhitespace.ReplaceAllString(cleaned, " ")
	cleaned = strings.TrimSpace(cleaned)
	return cleaned
}

// findConfigurationFile remains the same.
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

// processURL is the worker that parses the initial page.
func processURL(arg string, cfg *ini.File, wg *sync.WaitGroup, results chan<- ProcessResult, keepTemporary bool) {
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

	_, err = io.Copy(tmpFile, resp.Body)
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpFile.Name())
		res.Err = fmt.Errorf("failed to write to temp file: %w", err)
		results <- res
		return
	}
	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		os.Remove(tmpFile.Name())
		res.Err = fmt.Errorf("failed to read temp file: %w", err)
		results <- res
		return
	}

	if hasPrefixRegex {
		re, err := regexp.Compile(prefixValue)
		if err != nil {
			res.Err = fmt.Errorf("invalid prefix regex '%s': %w", prefixValue, err)
			results <- res
			return
		}
		matches := re.FindSubmatch(content)
		if len(matches) >= 2 {
			res.FinalPrefix = string(matches[1])
			res.OutputMessages = append(res.OutputMessages, "  Prefix extracted from content.")
		} else {
			res.OutputMessages = append(res.OutputMessages, "  Prefix regex did not match or had no capture group.")
		}
	}
	for _, key := range section.Keys() {
		if strings.HasPrefix(key.Name(), "re") {
			regexString := key.String()
			re, err := regexp.Compile(regexString)
			if err != nil {
				res.OutputMessages = append(res.OutputMessages, fmt.Sprintf("  Warning: Invalid regex for key '%s': %v", key.Name(), err))
				continue
			}
			matches := re.FindAllSubmatch(content, -1)
			for _, match := range matches {
				if len(match) > 1 {
					res.FoundURLs = append(res.FoundURLs, string(match[1]))
				}
			}
		}
	}

	// --- MODIFIED LOGIC for keeping the temporary file ---
	if keepTemporary {
		timestamp := time.Now().UnixNano()
		// Use the matched section name for a cleaner filename
		newFileName := fmt.Sprintf("%s-%d.html", sectionName, timestamp)

		err := os.Rename(tmpFile.Name(), newFileName)
		if err != nil {
			res.OutputMessages = append(res.OutputMessages, fmt.Sprintf("  Warning: Failed to keep temporary file: %v", err))
			os.Remove(tmpFile.Name())
		} else {
			res.OutputMessages = append(res.OutputMessages, fmt.Sprintf("  Kept temporary file as %s", newFileName))
		}
	} else {
		os.Remove(tmpFile.Name())
	}

	res.FinalPrefix = cleanPrefix(res.FinalPrefix)
	results <- res
}

// downloadURL function remains the same.
func downloadURL(url, filepath string, wg *sync.WaitGroup, results chan<- DownloadResult) {
	defer wg.Done()
	res := DownloadResult{URL: url, Filepath: filepath}

	resp, err := http.Get(url)
	if err != nil {
		res.Err = err
		results <- res
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		res.Err = fmt.Errorf("bad status: %s", resp.Status)
		results <- res
		return
	}

	out, err := os.Create(filepath)
	if err != nil {
		res.Err = err
		results <- res
		return
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		res.Err = err
	}
	results <- res
}

// main function remains the same.
func main() {
	verbose := flag.Bool("v", false, "Enable verbose output")
	flag.BoolVar(verbose, "verbose", false, "Enable verbose output")
	keepTemporary := flag.Bool("k", false, "Keep temporary downloaded files for debugging")
	flag.BoolVar(keepTemporary, "keep", false, "Keep temporary downloaded files for debugging")
	flag.BoolVar(keepTemporary, "keep-temporary", false, "Keep temporary downloaded files for debugging")
	flag.Parse()

	configFile, err := findConfigurationFile()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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

	parseResults := make(chan ProcessResult, len(args))
	var parseWg sync.WaitGroup
	for _, arg := range args {
		parseWg.Add(1)
		go processURL(arg, cfg, &parseWg, parseResults, *keepTemporary)
	}
	parseWg.Wait()
	close(parseResults)

	var processed []ProcessResult
	for res := range parseResults {
		processed = append(processed, res)
	}

	if *verbose {
		fmt.Println("\n--- Starting Download Phase ---")
	}
	downloadResults := make(chan DownloadResult)
	var downloadWg sync.WaitGroup
	totalDownloads := 0

	for _, res := range processed {
		if *verbose {
			for _, msg := range res.OutputMessages {
				fmt.Println(msg)
			}
			fmt.Printf("--- Queuing downloads for %s ---\n", res.URL)
		}
		if res.Err != nil {
			fmt.Fprintf(os.Stderr, "Error processing this URL: %v\n", res.Err)
			continue
		}
		if len(res.FoundURLs) == 0 {
			if *verbose {
				fmt.Println("No URLs found to download.")
			}
			continue
		}

		for i, urlToDownload := range res.FoundURLs {
			fileIndex := i + 1
			extension := path.Ext(urlToDownload)
			if extension == "" {
				extension = ".unknown"
			}
			fileName := fmt.Sprintf("%s-%02d%s", res.FinalPrefix, fileIndex, extension)

			downloadWg.Add(1)
			totalDownloads++
			go downloadURL(urlToDownload, fileName, &downloadWg, downloadResults)
		}
	}

	go func() {
		downloadWg.Wait()
		close(downloadResults)
	}()

	for i := 0; i < totalDownloads; i++ {
		res := <-downloadResults
		if res.Err != nil {
			fmt.Fprintf(os.Stderr, "Error downloading %s: %v\n", res.URL, res.Err)
		} else {
			if *verbose {
				fmt.Printf("%s -> %s\n", res.URL, res.Filepath)
			} else {
				fmt.Println(res.Filepath)
			}
		}
	}
	if *verbose {
		fmt.Println("--- Download Phase Complete ---")
	}
}
