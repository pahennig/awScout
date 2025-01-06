// services/cloudformationutils.go

package services

import (
	"context"
	"fmt"
	"log"
	"sync"

	"awsecrets/formatting"
	"awsecrets/pattern"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfTypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
)

type StackData struct {
	StackName    string
	StackID      string
	TemplateBody string
	Parameters   []cfTypes.Parameter
}

type StackSetData struct {
	StackSetName string
	StackSetId   string
	TemplateBody string
	Parameters   []cfTypes.Parameter
}

func FetchStacks(ctx context.Context, cfClient *cloudformation.Client, threads int) ([]StackData, error) {
	var stacks []StackData
	var mu sync.Mutex
	var wg sync.WaitGroup

	desiredStatuses := []cfTypes.StackStatus{
		cfTypes.StackStatusCreateComplete,
		cfTypes.StackStatusUpdateComplete,
		cfTypes.StackStatusUpdateRollbackComplete,
		cfTypes.StackStatusImportComplete,
		cfTypes.StackStatusImportRollbackComplete,
		cfTypes.StackStatusDeleteComplete, // Include deleted stacks - According to https://docs.aws.amazon.com/AWSCloudFormation/latest/APIReference/API_GetTemplate.html, the resource is available for 90 days after its removal
		cfTypes.StackStatusDeleteFailed,
	}

	paginator := cloudformation.NewListStacksPaginator(cfClient, &cloudformation.ListStacksInput{
		StackStatusFilter: desiredStatuses,
	})

	semaphore := make(chan struct{}, threads)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get stacks page: %w", err)
		}

		for _, stackSummary := range page.StackSummaries {
			wg.Add(1)
			semaphore <- struct{}{}
			go func(summary cfTypes.StackSummary) {
				defer wg.Done()
				defer func() { <-semaphore }()

				stackName := aws.ToString(summary.StackName)
				stackID := aws.ToString(summary.StackId)

				templateOutput, err := cfClient.GetTemplate(ctx, &cloudformation.GetTemplateInput{
					StackName: aws.String(stackID),
				})
				if err != nil {
					log.Printf("Failed to get template for stack %s: %v", stackName, err)
					return
				}

				templateBody := aws.ToString(templateOutput.TemplateBody)

				// Fetch stack parameters
				describeOutput, err := cfClient.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
					StackName: aws.String(stackID),
				})
				if err != nil {
					log.Printf("Failed to describe stack %s: %v", stackName, err)
					return
				}

				var parameters []cfTypes.Parameter
				if len(describeOutput.Stacks) > 0 {
					parameters = describeOutput.Stacks[0].Parameters
				}

				mu.Lock()
				stacks = append(stacks, StackData{
					StackName:    stackName,
					StackID:      stackID,
					TemplateBody: templateBody,
					Parameters:   parameters,
				})
				mu.Unlock()
			}(stackSummary)
		}
	}

	wg.Wait()
	return stacks, nil
}

func FetchStackSets(ctx context.Context, cfClient *cloudformation.Client, threads int) ([]StackSetData, error) {
	var stackSets []StackSetData
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, 1)

	paginator := cloudformation.NewListStackSetsPaginator(cfClient, &cloudformation.ListStackSetsInput{})

	stackSetChan := make(chan cfTypes.StackSetSummary, threads)

	// Start worker goroutines
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for summary := range stackSetChan {
				stackSetName := aws.ToString(summary.StackSetName)
				stackSetId := aws.ToString(summary.StackSetId)

				describeOutput, err := cfClient.DescribeStackSet(ctx, &cloudformation.DescribeStackSetInput{
					StackSetName: aws.String(stackSetId),
				})
				if err != nil {
					log.Printf("Failed to describe stack set %s (%s): %v", stackSetName, stackSetId, err)
					continue
				}

				templateBody := aws.ToString(describeOutput.StackSet.TemplateBody)
				parameters := describeOutput.StackSet.Parameters

				mu.Lock()
				stackSets = append(stackSets, StackSetData{
					StackSetName: stackSetName,
					StackSetId:   stackSetId,
					TemplateBody: templateBody,
					Parameters:   parameters,
				})
				mu.Unlock()
			}
		}()
	}

	// Feed stack set summaries to the channel
	go func() {
		defer close(stackSetChan)
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				errChan <- fmt.Errorf("failed to get stack sets page: %w", err)
				return
			}
			for _, summary := range page.Summaries {
				select {
				case stackSetChan <- summary:
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
		return stackSets, nil
	}
}

func ProcessCloudFormation(stacks []StackData, stackSets []StackSetData, patternMatcher *pattern.Patterns, showContent bool, matchMode string) {
	for _, stack := range stacks {
		matches := patternMatcher.MatchPatterns(stack.TemplateBody, matchMode)
		if len(matches) > 0 {
			//formatting.StackName(stack.StackName)
			formatting.Title("CloudFormation Stack", stack.StackName)
			for patternName, matchedStrings := range matches {
				for _, match := range matchedStrings {
					formatting.PatterName(patternName)
					formatting.Content(match, showContent)
				}
			}
			//fmt.Println("Parameters:")
			for _, param := range stack.Parameters {
				paramKey := aws.ToString(param.ParameterKey)
				paramValue := aws.ToString(param.ParameterValue)
				matchesKey := patternMatcher.MatchPatterns(paramKey, matchMode)
				matchesValue := patternMatcher.MatchPatterns(paramValue, matchMode)
				if len(matchesKey) > 0 || len(matchesValue) > 0 {
					formatting.CloudformationParameter()
					//formatting.PatterName(paramKey)
					//formatting.Content(paramValue, showContent)
					for patternName, matchedStrings := range matchesKey {
						for _, match := range matchedStrings {
							formatting.PatterName(patternName)
							formatting.Content(match, showContent)
						}
					}
					for patternName, matchedStrings := range matchesValue {
						for _, match := range matchedStrings {
							formatting.PatterName(patternName)
							formatting.Content(match, showContent)
						}
					}
				}
			}
			fmt.Println()
		}
	}

	for _, stackSet := range stackSets {
		matches := patternMatcher.MatchPatterns(stackSet.TemplateBody, matchMode)
		if len(matches) > 0 {
			//formatting.StackSetName(stackSet.StackSetName)
			formatting.Title("CloudFormation Stack Set", stackSet.StackSetName)
			for patternName, matchedStrings := range matches {
				for _, match := range matchedStrings {
					formatting.PatterName(patternName)
					formatting.Content(match, showContent)
				}
			}

			if len(stackSet.Parameters) != 0 {
				fmt.Println("Parameters:")
				for _, param := range stackSet.Parameters {
					fmt.Printf("  %s: %s\n", aws.ToString(param.ParameterKey), aws.ToString(param.ParameterValue))
				}
			}
			fmt.Println()
		}
	}
}
