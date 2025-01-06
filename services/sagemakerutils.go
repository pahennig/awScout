package services

import (
	"awsecrets/formatting"
	"awsecrets/pattern"
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sagemaker"
	"github.com/aws/smithy-go"
)

type SageMakerProcessingJobData struct {
	Name        string
	Description string
	Environment map[string]string
}

func FetchSageMakerProcessingJobs(ctx context.Context, client *sagemaker.Client, threads int) ([]SageMakerProcessingJobData, error) {
	var jobs []SageMakerProcessingJobData
	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, threads)

	// Initialize counters
	totalJobs := 0
	successfulJobs := 0

	paginator := sagemaker.NewListProcessingJobsPaginator(client, &sagemaker.ListProcessingJobsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, job := range page.ProcessingJobSummaries {
			wg.Add(1)
			semaphore <- struct{}{}

			// Increment the total job count
			totalJobs++

			go func(jobName string) {
				defer wg.Done()
				defer func() { <-semaphore }()

				var jobDetail *sagemaker.DescribeProcessingJobOutput
				var err error
				maxRetries := 5
				for i := 0; i < maxRetries; i++ {
					jobDetail, err = client.DescribeProcessingJob(ctx, &sagemaker.DescribeProcessingJobInput{
						ProcessingJobName: &jobName,
					})
					if err == nil {
						break
					}

					var ae smithy.APIError
					if errors.As(err, &ae) {
						if ae.ErrorCode() == "ThrottlingException" {
							waitTime := time.Duration(int(math.Pow(2, float64(i)))) * time.Second
							fmt.Printf("Throttling occurred for job %s. Retrying in %v...\n", jobName, waitTime)
							time.Sleep(waitTime)
							continue
						}
					}

					fmt.Printf("Error fetching details for job %s: %v\n", jobName, err)
					return
				}

				if err != nil {
					fmt.Printf("Max retries reached. Error fetching details for job %s: %v\n", jobName, err)
					return
				}

				jobData := SageMakerProcessingJobData{
					Name:        jobName,
					Description: *jobDetail.ProcessingJobName,
					Environment: jobDetail.Environment,
				}

				mu.Lock()
				jobs = append(jobs, jobData)
				// Increment successful job count
				successfulJobs++
				mu.Unlock()
			}(*job.ProcessingJobName)
		}
	}

	wg.Wait()

	// Print the total number of jobs and the number of successfully fetched jobs
	fmt.Printf("Total number of SageMaker Processing Jobs: %d\n", totalJobs)
	fmt.Printf("Number of successfully fetched SageMaker Processing Jobs: %d\n", successfulJobs)

	return jobs, nil
}

func ProcessSageMakerJobs(jobs []SageMakerProcessingJobData, patternMatcher *pattern.Patterns, showContent bool, matchMode string) {
	for _, job := range jobs {
		matches := patternMatcher.MatchPatterns(job.Description, matchMode)
		if len(matches) > 0 {
			formatting.Title("SageMaker job", job.Name)
			for pattern, matchList := range matches {
				for _, match := range matchList {
					formatting.PatterName(pattern)
					formatting.Content(match, showContent)
				}
			}
		}

		envMatches := matchEnvVariables(job.Environment, patternMatcher, matchMode)
		if len(envMatches) > 0 {
			//fmt.Printf("  Found matches in environment variables:\n")
			for envVar, matchList := range envMatches {
				for _, match := range matchList {
					formatting.Title("SageMaker job", job.Name)
					formatting.Data("SageMaker environment", envVar)
					formatting.Content(match, showContent)
				}
			}
		}
	}
}
