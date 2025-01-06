package services

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"sync"

	"awsecrets/formatting"
	"awsecrets/pattern"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	glueTypes "github.com/aws/aws-sdk-go-v2/service/glue/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type GlueJobData struct {
	JobName       string
	Script        string
	ScriptContent string
	JobParams     map[string]string
}

func FetchGlueJobs(ctx context.Context, glueClient *glue.Client, s3Client *s3.Client, threads int) ([]GlueJobData, error) {
	var jobs []GlueJobData
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, 1)

	paginator := glue.NewListJobsPaginator(glueClient, &glue.ListJobsInput{})

	jobChan := make(chan glueTypes.Job, threads)

	// Start worker goroutines
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobChan {
				jobName := aws.ToString(job.Name)

				// Get job details
				jobOutput, err := glueClient.GetJob(ctx, &glue.GetJobInput{
					JobName: aws.String(jobName),
				})
				if err != nil {
					log.Printf("Failed to get job details for %s: %v", jobName, err)
					continue
				}

				script := aws.ToString(jobOutput.Job.Command.ScriptLocation)
				scriptContent, err := downloadS3Script(ctx, s3Client, script)
				if err != nil {
					log.Printf("Failed to download script for job %s: %v", jobName, err)
					scriptContent = ""
				}
				jobParams := make(map[string]string)
				for k, v := range jobOutput.Job.DefaultArguments {
					jobParams[k] = v
				}

				mu.Lock()
				jobs = append(jobs, GlueJobData{
					JobName:       jobName,
					Script:        script,
					ScriptContent: scriptContent,
					JobParams:     jobParams,
				})
				mu.Unlock()
			}
		}()
	}

	// Feed jobs to the channel
	go func() {
		defer close(jobChan)
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				errChan <- fmt.Errorf("failed to get jobs page: %w", err)
				return
			}
			for _, job := range page.JobNames {
				select {
				case jobChan <- glueTypes.Job{Name: aws.String(job)}:
				case <-ctx.Done():
					errChan <- ctx.Err()
					return
				}
			}
		}
	}()

	wg.Wait()

	select {
	case err := <-errChan:
		return nil, err
	default:
		return jobs, nil
	}
}

func downloadS3Script(ctx context.Context, s3Client *s3.Client, scriptLocation string) (string, error) {
	parts := strings.SplitN(strings.TrimPrefix(scriptLocation, "s3://"), "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid S3 URL: %s", scriptLocation)
	}
	bucket := parts[0]
	key := parts[1]

	result, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", err
	}
	defer result.Body.Close()

	content, err := ioutil.ReadAll(result.Body)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

func ProcessGlueJobs(jobs []GlueJobData, patternMatcher *pattern.Patterns, showContent bool, matchMode string) {
	for _, job := range jobs {
		scriptLocationMatches := patternMatcher.MatchPatterns(job.Script, matchMode)
		scriptContentMatches := patternMatcher.MatchPatterns(job.ScriptContent, matchMode)
		paramsMatches := matchJobParameters(job.JobParams, patternMatcher, matchMode)

		if len(scriptLocationMatches) > 0 || len(scriptContentMatches) > 0 || len(paramsMatches) > 0 {
			//formatting.GlueJobName(job.JobName)
			formatting.Title("Glue Job", job.JobName)

			if len(scriptLocationMatches) > 0 {
				formatting.FuncCodeDetails("Script Location", scriptLocationMatches, showContent)
			}

			if len(scriptContentMatches) > 0 {
				formatting.FuncCodeDetails("Script Content", scriptContentMatches, showContent)
			}

			if len(paramsMatches) > 0 {
				formatting.FuncCodeDetails("Job Parameters", paramsMatches, showContent)
			}

			fmt.Println()
		}
	}
}

func matchJobParameters(params map[string]string, patternMatcher *pattern.Patterns, matchMode string) map[string][]string {
	matches := make(map[string][]string)
	for key, value := range params {
		paramMatches := patternMatcher.MatchPatterns(value, matchMode)
		for pattern := range paramMatches {
			matches[pattern] = append(matches[pattern], fmt.Sprintf("%s: %s", key, value))
		}
	}
	return matches
}
