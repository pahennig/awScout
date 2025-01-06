package services

import (
	"awsecrets/formatting"
	"awsecrets/pattern"
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
)

type CodeBuildProjectData struct {
	ProjectName string
	Source      string
	Environment map[string]string
	Buildspec   string
}

func FetchCodeBuildProjects(ctx context.Context, codebuildClient *codebuild.Client, threads int) ([]CodeBuildProjectData, error) {
	var projects []CodeBuildProjectData
	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, threads)

	paginator := codebuild.NewListProjectsPaginator(codebuildClient, &codebuild.ListProjectsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, projectName := range page.Projects {
			wg.Add(1)
			go func(projectName string) {
				semaphore <- struct{}{}
				defer func() {
					<-semaphore
					wg.Done()
				}()

				project, err := codebuildClient.BatchGetProjects(ctx, &codebuild.BatchGetProjectsInput{
					Names: []string{projectName},
				})
				if err != nil {
					log.Printf("Failed to get project details for %s: %v", projectName, err)
					return
				}

				if len(project.Projects) > 0 {
					p := project.Projects[0]
					source := aws.ToString(p.Source.Location)
					environment := make(map[string]string)
					for _, env := range p.Environment.EnvironmentVariables {
						environment[aws.ToString(env.Name)] = aws.ToString(env.Value)
					}

					mu.Lock()
					buildspec := aws.ToString(p.Source.Buildspec)
					if buildspec == "" {
						buildspec = "buildspec.yml"
					}
					projects = append(projects, CodeBuildProjectData{
						ProjectName: projectName,
						Source:      source,
						Environment: environment,
						Buildspec:   buildspec,
					})
					mu.Unlock()
				}
			}(projectName)
		}
	}

	wg.Wait()
	return projects, nil
}

func ProcessCodeBuildProjects(projects []CodeBuildProjectData, patternMatcher *pattern.Patterns, showContent bool, matchMode string) {
	for _, project := range projects {
		var hasMatches bool
		matchesInSource := patternMatcher.MatchPatterns(project.Source, matchMode)
		matchesInEnvVars := matchEnvVariables(project.Environment, patternMatcher, matchMode)
		matchesInBuildspec := patternMatcher.MatchPatterns(project.Buildspec, matchMode)

		if len(matchesInSource) > 0 || len(matchesInEnvVars) > 0 || len(matchesInBuildspec) > 0 {
			hasMatches = true
		}

		if hasMatches {
			//formatting.PatterName(project.ProjectName)
			formatting.Title("CodeBuild Project", project.ProjectName)

			if len(matchesInSource) > 0 {
				formatting.FuncCodeDetails("Source", matchesInSource, showContent)
			}

			if len(matchesInEnvVars) > 0 {
				formatting.FuncCodeDetails("Environment Variables", matchesInEnvVars, showContent)
			}

			if len(matchesInBuildspec) > 0 {
				formatting.FuncCodeDetails("Buildspec", matchesInBuildspec, showContent)
			}

			fmt.Println()
		}
	}
}
