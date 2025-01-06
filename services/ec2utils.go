package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strconv"
	"sync"

	"awsecrets/formatting"
	"awsecrets/pattern"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// InstanceData holds the instance ID and its user data
type InstanceData struct {
	InstanceID string
	UserData   string
}

type LaunchTemplateData struct {
	TemplateName string
	TemplateID   string
	Version      int64
	UserData     string
}

// FetchInstances retrieves EC2 instances and their user data
func FetchInstances(ctx context.Context, ec2client *ec2.Client, threads int) ([]InstanceData, error) {
	var instances []InstanceData
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, 1)

	paginator := ec2.NewDescribeInstancesPaginator(ec2client, &ec2.DescribeInstancesInput{})

	instanceChan := make(chan ec2types.Instance, threads)

	// Start worker goroutines
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for instance := range instanceChan {
				instanceID := *instance.InstanceId

				// Get user data
				attrOutput, err := ec2client.DescribeInstanceAttribute(ctx, &ec2.DescribeInstanceAttributeInput{
					InstanceId: aws.String(instanceID),
					Attribute:  ec2types.InstanceAttributeNameUserData,
				})
				if err != nil {
					log.Printf("Failed to get user data for instance %s: %v", instanceID, err)
					continue
				}

				// Decode user data
				var userData string
				if attrOutput.UserData != nil && attrOutput.UserData.Value != nil {
					userDataEncoded := *attrOutput.UserData.Value
					userDataBytes, err := base64.StdEncoding.DecodeString(userDataEncoded)
					if err != nil {
						log.Printf("Failed to decode user data for instance %s: %v", instanceID, err)
						continue
					}
					userData = string(userDataBytes)
				}

				mu.Lock()
				instances = append(instances, InstanceData{
					InstanceID: instanceID,
					UserData:   userData,
				})
				mu.Unlock()
			}
		}()
	}

	// Feed instances to the channel
	go func() {
		defer close(instanceChan)
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				errChan <- fmt.Errorf("failed to get instances page: %w", err)
				return
			}
			for _, reservation := range page.Reservations {
				for _, instance := range reservation.Instances {
					select {
					case instanceChan <- instance:
					case <-ctx.Done():
						errChan <- ctx.Err()
						return
					}
				}
			}
		}
	}()

	wg.Wait()

	select {
	case err := <-errChan:
		return nil, err
	default:
		return instances, nil
	}
}

func ProcessInstances(instances []InstanceData, patternMatcher *pattern.Patterns, showContent bool, matchMode string) {
	for _, instance := range instances {
		matches := patternMatcher.MatchPatterns(instance.UserData, matchMode)
		if len(matches) > 0 {
			formatting.Title("Instance ID", instance.InstanceID)
			for patterName, matchedStrings := range matches {
				for _, match := range matchedStrings {
					formatting.PatterName(patterName)
					formatting.Content(match, showContent)
				}
			}
		}
	}
	fmt.Println()
}

// Launch Template
func FetchLaunchTemplates(ctx context.Context, ec2Client *ec2.Client, threads int) ([]LaunchTemplateData, error) {
	var templates []LaunchTemplateData
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, 1)

	// Paginator to list all launch templates
	ltPaginator := ec2.NewDescribeLaunchTemplatesPaginator(ec2Client, &ec2.DescribeLaunchTemplatesInput{})

	templateChan := make(chan ec2types.LaunchTemplate, threads)

	// Start worker goroutines
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for lt := range templateChan {
				launchTemplateName := aws.ToString(lt.LaunchTemplateName)
				launchTemplateID := aws.ToString(lt.LaunchTemplateId)

				// For each launch template, get all versions
				ltvPaginator := ec2.NewDescribeLaunchTemplateVersionsPaginator(ec2Client, &ec2.DescribeLaunchTemplateVersionsInput{
					LaunchTemplateId: aws.String(launchTemplateID),
				})

				for ltvPaginator.HasMorePages() {
					ltvPage, err := ltvPaginator.NextPage(ctx)
					if err != nil {
						errChan <- fmt.Errorf("failed to get launch template versions page: %w", err)
						return
					}

					for _, version := range ltvPage.LaunchTemplateVersions {
						versionNumber := aws.ToInt64(version.VersionNumber)

						var userData string
						if version.LaunchTemplateData != nil && version.LaunchTemplateData.UserData != nil {
							userDataEncoded := aws.ToString(version.LaunchTemplateData.UserData)
							// Decode the base64-encoded user data
							userDataBytes, err := base64.StdEncoding.DecodeString(userDataEncoded)
							if err != nil {
								log.Printf("Failed to decode user data for template %s version %d: %v", launchTemplateName, versionNumber, err)
								continue
							}
							userData = string(userDataBytes)
						}

						mu.Lock()
						templates = append(templates, LaunchTemplateData{
							TemplateName: launchTemplateName,
							TemplateID:   launchTemplateID,
							Version:      versionNumber,
							UserData:     userData,
						})
						mu.Unlock()
					}
				}
			}
		}()
	}

	// Feed launch templates to the channel
	go func() {
		defer close(templateChan)
		for ltPaginator.HasMorePages() {
			ltPage, err := ltPaginator.NextPage(ctx)
			if err != nil {
				errChan <- fmt.Errorf("failed to get launch templates page: %w", err)
				return
			}
			for _, lt := range ltPage.LaunchTemplates {
				select {
				case templateChan <- lt:
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
		return templates, nil
	}
}

func ProcessLaunchTemplates(templates []LaunchTemplateData, patternMatcher *pattern.Patterns, showContent bool, matchMode string) {
	// Group templates by their TemplateName
	templatesByName := make(map[string][]LaunchTemplateData)
	for _, template := range templates {
		templatesByName[template.TemplateName] = append(templatesByName[template.TemplateName], template)
	}

	// Iterate over each launch template
	for templateName, versions := range templatesByName {
		var templateMatches []struct {
			Version int64
			Matches map[string][]string
		}

		// Check each version for matches
		for _, template := range versions {
			matches := patternMatcher.MatchPatterns(template.UserData, matchMode)
			if len(matches) > 0 {
				// Collect versions with matches
				templateMatches = append(templateMatches, struct {
					Version int64
					Matches map[string][]string
				}{
					Version: template.Version,
					Matches: matches,
				})
			}
		}

		// Only print if there are matches in any version
		if len(templateMatches) > 0 {
			// Print the launch template name once
			formatting.Title("Launch Template", templateName)
			for _, tm := range templateMatches {
				// Print the version information
				//formatting.LaunchTemplateData("Version", strconv.Itoa(tm.Version))
				formatting.Data("Version", strconv.Itoa(int(tm.Version)))
				for patternName, matchedStrings := range tm.Matches {
					for _, match := range matchedStrings {
						formatting.PatterName(patternName)
						formatting.Content(match, showContent)
					}
				}
			}
			fmt.Println() // Add an empty line between different launch templates
		}
	}
}
