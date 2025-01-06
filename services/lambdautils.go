package services

import (
	"archive/zip"
	"awsecrets/formatting"
	"awsecrets/pattern"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdaTypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

type LambdaFunctionData struct {
	FunctionName string
	Version      string
	Code         string
	EnvVariables map[string]string
}

func FetchLambdaFunctions(ctx context.Context, lambdaclient *lambda.Client, threads int) ([]LambdaFunctionData, error) {
	var functions []LambdaFunctionData
	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, threads)

	// Paginator to list all functions
	paginator := lambda.NewListFunctionsPaginator(lambdaclient, &lambda.ListFunctionsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, function := range page.Functions {
			wg.Add(1)
			go func(function lambdaTypes.FunctionConfiguration) {
				semaphore <- struct{}{}
				defer func() {
					<-semaphore
					wg.Done()
				}()
				functionName := aws.ToString(function.FunctionName)

				// Collect all versions of the function
				var versions []lambdaTypes.FunctionConfiguration

				versionPaginator := lambda.NewListVersionsByFunctionPaginator(lambdaclient, &lambda.ListVersionsByFunctionInput{
					FunctionName: aws.String(functionName),
				})

				for versionPaginator.HasMorePages() {
					versionPage, err := versionPaginator.NextPage(ctx)
					if err != nil {
						log.Printf("Failed to list versions for function %s: %v", functionName, err)
						return
					}

					versions = append(versions, versionPage.Versions...)
				}

				// Reverse the versions slice to have the latest versions first
				reverseVersions(versions)

				// Process each version starting from the latest
				for _, version := range versions {
					versionNumber := aws.ToString(version.Version)

					codeOutput, err := lambdaclient.GetFunction(ctx, &lambda.GetFunctionInput{
						FunctionName: aws.String(functionName),
						Qualifier:    aws.String(versionNumber),
					})
					if err != nil {
						log.Printf("Failed to get function code for %s version %s: %v", functionName, versionNumber, err)
						continue
					}

					codeLocation := aws.ToString(codeOutput.Code.Location)
					if codeLocation == "" {
						log.Printf("Empty code location for function %s version %s", functionName, versionNumber)
					}
					code := fetchAndDecodeCode(codeLocation)

					var envVars map[string]string
					if codeOutput.Configuration.Environment != nil && codeOutput.Configuration.Environment.Variables != nil {
						envVars = codeOutput.Configuration.Environment.Variables
					}

					mu.Lock()
					functions = append(functions, LambdaFunctionData{
						FunctionName: functionName,
						Version:      versionNumber,
						Code:         code,
						EnvVariables: envVars,
					})
					mu.Unlock()
				}
			}(function)
		}
	}

	wg.Wait()
	return functions, nil
}

func fetchAndDecodeCode(codeLocation string) string {
	if codeLocation == "" {
		log.Printf("Empty code location provided")
		return ""
	}
	resp, err := http.Get(codeLocation)
	if err != nil {
		log.Printf("Failed to download Lambda code from %s: %v", codeLocation, err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to download Lambda code: %s returned status %d", codeLocation, resp.StatusCode)
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read response body: %v", err)
		return ""
	}

	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		log.Printf("Failed to unzip Lambda code: %v", err)
		return ""
	}

	var lambdaCode string
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			log.Printf("Failed to open file %s in zip archive: %v", file.Name, err)
			continue
		}

		fileContent, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			log.Printf("Failed to read file %s content: %v", file.Name, err)
			continue
		}

		lambdaCode += string(fileContent) + "\n"
	}
	return lambdaCode
}

func ProcessLambdas(functions []LambdaFunctionData, patternMatcher *pattern.Patterns, showContent bool, matchMode string) {
	// Group functions by function name
	functionsByName := make(map[string][]LambdaFunctionData)
	for _, function := range functions {
		functionsByName[function.FunctionName] = append(functionsByName[function.FunctionName], function)
	}

	for functionName, versions := range functionsByName {
		// Sort the versions to have $LATEST first, then descending version numbers
		sort.SliceStable(versions, func(i, j int) bool {
			vi := versions[i].Version
			vj := versions[j].Version

			// Handle $LATEST
			if vi == "$LATEST" {
				return true
			}
			if vj == "$LATEST" {
				return false
			}

			// Convert version strings to integers
			viInt, err1 := strconv.Atoi(vi)
			vjInt, err2 := strconv.Atoi(vj)
			if err1 != nil || err2 != nil {
				// If conversion fails, fall back to string comparison
				return vi > vj
			}

			// Sort in descending order
			return viInt > vjInt
		})

		var hasMatches bool
		var versionMatches []struct {
			Version          string
			MatchesInCode    map[string][]string
			MatchesInEnvVars map[string][]string
		}

		// Process each version
		for _, function := range versions {
			matchesInCode := patternMatcher.MatchPatterns(function.Code, matchMode)
			matchesInEnvVars := matchEnvVariables(function.EnvVariables, patternMatcher, matchMode)

			if len(matchesInCode) > 0 || len(matchesInEnvVars) > 0 {
				hasMatches = true
				versionMatches = append(versionMatches, struct {
					Version          string
					MatchesInCode    map[string][]string
					MatchesInEnvVars map[string][]string
				}{
					Version:          function.Version,
					MatchesInCode:    matchesInCode,
					MatchesInEnvVars: matchesInEnvVars,
				})
			}
		}

		if hasMatches {
			// Display the function name once
			//formatting.LambdaName(functionName)
			formatting.Title("Lambda Function", functionName)

			// Display matches for each version
			for _, vm := range versionMatches {
				//fmt.Printf("Version: %s\n", vm.Version)
				//formatting.LambdaVersion(vm.Version)
				formatting.Data("Version", vm.Version)

				if len(vm.MatchesInCode) > 0 {
					formatting.FuncCodeDetails("", vm.MatchesInCode, showContent)
				}

				if len(vm.MatchesInEnvVars) > 0 {
					formatting.FuncCodeDetails("", vm.MatchesInEnvVars, showContent)
				}
			}
			fmt.Println()
		}
	}
}

func matchEnvVariables(envVars map[string]string, patternMatcher *pattern.Patterns, matchMode string) map[string][]string {
	matches := make(map[string][]string)
	for key, value := range envVars {
		// Match patterns in the environment variable key
		keyMatches := patternMatcher.MatchPatterns(key, matchMode)
		if len(keyMatches) > 0 {
			for patternName := range keyMatches {
				// For key matches, store the corresponding value as the match
				matches[fmt.Sprintf("Key Matched in env variable: %s (Pattern: %s)", key, patternName)] = []string{value}
			}
		}

		// Match patterns in the environment variable value
		valueMatches := patternMatcher.MatchPatterns(value, matchMode)
		for patternName, matchedStrings := range valueMatches {
			if len(matchedStrings) > 0 {
				// Store the value matches
				matches[fmt.Sprintf("Value of Key: %s (Pattern: %s)", key, patternName)] = matchedStrings
			}
		}
	}
	return matches
}

func reverseVersions(versions []lambdaTypes.FunctionConfiguration) {
	for i, j := 0, len(versions)-1; i < j; i, j = i+1, j-1 {
		versions[i], versions[j] = versions[j], versions[i]
	}
}
