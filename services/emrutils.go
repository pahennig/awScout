package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"awsecrets/formatting"
	"awsecrets/pattern"

	"github.com/aws/aws-sdk-go-v2/service/emr"
	"github.com/aws/aws-sdk-go-v2/service/emr/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type EMRClusterData struct {
	ClusterID               string
	Steps                   []types.StepSummary
	BootstrapActions        []types.Command
	BootstrapScriptContents []string
}

func FetchEMRClusters(ctx context.Context, emrClient *emr.Client, s3Client *s3.Client, threads int) ([]EMRClusterData, error) {
	var clusters []EMRClusterData

	params := &emr.ListClustersInput{
		ClusterStates: []types.ClusterState{
			types.ClusterStateRunning,
			types.ClusterStateWaiting,
			types.ClusterStateBootstrapping,
			types.ClusterStateStarting,
			//types.ClusterStateTerminated,
			//types.ClusterStateTerminatedWithErrors,
		},
		Marker: nil,
	}
	paginator := emr.NewListClustersPaginator(emrClient, params)

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list EMR clusters: %w", err)
		}

		for _, cluster := range output.Clusters {
			clusterData := EMRClusterData{
				ClusterID: *cluster.Id,
			}

			steps, err := fetchClusterSteps(ctx, emrClient, *cluster.Id)
			if err != nil {
				fmt.Printf("Error fetching steps for cluster %s: %v\n", *cluster.Id, err)
				continue
			}
			clusterData.Steps = steps

			bootstrapActions, scriptContents, err := fetchBootstrapActions(ctx, emrClient, s3Client, *cluster.Id)
			if err != nil {
				fmt.Printf("Error fetching bootstrap actions for cluster %s: %v\n", *cluster.Id, err)
				continue
			}
			clusterData.BootstrapActions = bootstrapActions
			clusterData.BootstrapScriptContents = scriptContents

			clusters = append(clusters, clusterData)
		}
	}

	return clusters, nil
}

func fetchClusterSteps(ctx context.Context, emrClient *emr.Client, clusterID string) ([]types.StepSummary, error) {
	params := &emr.ListStepsInput{
		ClusterId: &clusterID,
	}

	var resp *emr.ListStepsOutput
	var err error
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		resp, err = emrClient.ListSteps(ctx, params)
		if err == nil {
			break
		}

		var ae smithy.APIError
		if errors.As(err, &ae) {
			if ae.ErrorCode() == "ThrottlingException" {
				waitTime := time.Duration(int(math.Pow(2, float64(i)))) * time.Second
				fmt.Printf("Throttling occurred for cluster %s. Retrying in %v...\n", clusterID, waitTime)
				time.Sleep(waitTime)
				continue
			}
		}

		return nil, fmt.Errorf("failed to list steps for cluster %s: %w", clusterID, err)
	}

	if err != nil {
		return nil, fmt.Errorf("max retries reached. Failed to list steps for cluster %s: %w", clusterID, err)
	}

	return resp.Steps, nil
}

func fetchBootstrapActions(ctx context.Context, emrClient *emr.Client, s3Client *s3.Client, clusterID string) ([]types.Command, []string, error) {
	params := &emr.ListBootstrapActionsInput{
		ClusterId: &clusterID,
	}

	var resp *emr.ListBootstrapActionsOutput
	var err error
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		resp, err = emrClient.ListBootstrapActions(ctx, params)
		if err == nil {
			break
		}

		var ae smithy.APIError
		if errors.As(err, &ae) {
			if ae.ErrorCode() == "ThrottlingException" {
				waitTime := time.Duration(int(math.Pow(2, float64(i)))) * time.Second
				fmt.Printf("Throttling occurred for cluster %s. Retrying in %v...\n", clusterID, waitTime)
				time.Sleep(waitTime)
				continue
			}
		}

		return nil, nil, fmt.Errorf("failed to list bootstrap actions for cluster %s: %w", clusterID, err)
	}

	if err != nil {
		return nil, nil, fmt.Errorf("max retries reached. Failed to list bootstrap actions for cluster %s: %w", clusterID, err)
	}

	var scriptContents []string
	for _, action := range resp.BootstrapActions {
		if action.ScriptPath != nil && strings.HasPrefix(*action.ScriptPath, "s3://") {
			content, err := downloadS3Script(ctx, s3Client, *action.ScriptPath)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to download bootstrap script for cluster %s: %w", clusterID, err)
			}
			scriptContents = append(scriptContents, content)
		} else {
			scriptContents = append(scriptContents, "") // Add empty string for non-S3 or empty script paths
		}
	}
	//fmt.Println(scriptContents)
	return resp.BootstrapActions, scriptContents, nil
}

func ProcessEMRClusters(clusters []EMRClusterData, patternMatcher *pattern.Patterns, showContent bool, matchMode string) {
	for _, cluster := range clusters {
		var hasMatches bool

		// Process bootstrap actions
		for i, action := range cluster.BootstrapActions {
			args := strings.Join(action.Args, " ")
			matches := patternMatcher.MatchPatterns(args, matchMode)
			if len(matches) > 0 || (i < len(cluster.BootstrapScriptContents) && len(patternMatcher.MatchPatterns(cluster.BootstrapScriptContents[i], matchMode)) > 0) {
				if !hasMatches {
					//formatting.EMRClusterID(cluster.ClusterID)
					formatting.Title("EMR Cluster ID", cluster.ClusterID)
					//fmt.Printf("Cluster ID: %s\n", cluster.ID)
					//fmt.Printf("Cluster Name: %s\n", cluster.)
					hasMatches = true
				}
				//fmt.Printf("  Bootstrap Action: %s\n", *action.Name)
				//formatting.EMRStepName(*action.Name)
				formatting.Data("EMR Step Name", *action.Name)
				//fmt.Printf("    Args: %s\n", args)

				//for pattern, matchPositions := range matches {
				for pattern, _ := range matches {
					formatting.PatterName(pattern)
					//fmt.Printf("    Matched pattern '%s' at positions: %v\n", pattern, matchPositions)
				}
				if i < len(cluster.BootstrapScriptContents) {
					scriptMatches := patternMatcher.MatchPatterns(cluster.BootstrapScriptContents[i], matchMode)
					//for pattern, matchPositions := range scriptMatches {
					for patternName, matchedStrings := range scriptMatches {
						for _, match := range matchedStrings {
							formatting.PatterName(patternName)
							//formatting.EMRScriptPath(*action.ScriptPath)
							formatting.Data("EMR Script Path", *action.ScriptPath)
							formatting.Content(match, showContent)
						}
					}
					// for _, matchPositions := range scriptMatches {
					// 	formatting.EMRScriptPath(*action.ScriptPath)
					// 	formatting.Content(string(matchPositions[0]), showContent)
					// 	//fmt.Printf("    Matched pattern '%s' in script content at positions: %v\n", pattern, matchPositions)
					// }
					// if showContent {
					// 	fmt.Printf("    Script Content:\n%s\n", cluster.BootstrapScriptContents[i])
					// }
				}
			}
		}

		// Process steps
		for _, step := range cluster.Steps {
			args := strings.Join(step.Config.Args, " ")
			matches := patternMatcher.MatchPatterns(args, matchMode)
			if len(matches) > 0 {
				if !hasMatches {
					//formatting.EMRClusterID(cluster.ClusterID)
					formatting.Title("EMR Cluster ID", cluster.ClusterID)
					hasMatches = true
				}
				//formatting.EMRStepName(*step.Name)
				formatting.Data("EMR Step Name", *step.Name)
				for patternName, matchedStrings := range matches {
					for _, match := range matchedStrings {
						formatting.PatterName(patternName)
						formatting.Content(match, showContent)
					}
				}
			}
		}

		// Process bootstrap actions
		for i, action := range cluster.BootstrapActions {
			args := strings.Join(action.Args, " ")
			matches := patternMatcher.MatchPatterns(args, matchMode)
			if len(matches) > 0 {
				if !hasMatches {
					//formatting.EMRClusterID(cluster.ClusterID)
					formatting.Title("EMR Cluster ID", cluster.ClusterID)
					hasMatches = true
				}
				//formatting.EMRStepName(fmt.Sprintf("Bootstrap Action %d", i+1))
				formatting.Data("EMR Step Name", fmt.Sprintf("Bootstrap Action %d", i+1))
				for patternName, matchedStrings := range matches {
					for _, match := range matchedStrings {
						formatting.PatterName(patternName)
						formatting.Content(match, showContent)
					}
				}
			}
		}

		if hasMatches {
			fmt.Println()
		}
	}
}
