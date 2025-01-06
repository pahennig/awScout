package main

import (
	"awsecrets/constants"
	"awsecrets/pattern"
	"awsecrets/services"
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/codebuild"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/emr"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sagemaker"
)

type Config struct {
	Region      string
	Profile     string
	Search      string
	ServiceFlag string
	ShowContent bool
	Threads     int
	MatchMode   string
}

func loadConfig() *Config {
	cfg := &Config{}
	flag.StringVar(&cfg.Region, "region", "us-east-1", "AWS region")
	flag.StringVar(&cfg.Profile, "profile", "default", "AWS profile")
	flag.StringVar(&cfg.Search, "search", "pattern/findallstring.json", "Regex file")
	flag.StringVar(&cfg.ServiceFlag, "service", "ec2,cloudformation,sagemaker,emr,codebuild,glue", "Service(s) to be used (e.g., ec2,cloudformation). Use 'all' to process all services\n* ec2: Check user data and launch templates along with versioning\n* lambda: Check lambda code and environment variables\n* cloudformation: Check stacks and stacksets\n* codebuild: Check buildspec\n* glue: Check bootstrap actions, s3 scripts and cluster args\n* sagemaker: Check processing job environment\n* emr: Check EMR clusters with env variables\n*")
	flag.BoolVar(&cfg.ShowContent, "show", false, "Show full matched content")
	flag.IntVar(&cfg.Threads, "threads", 4, "Number of concurrent threads")
	flag.StringVar(&cfg.MatchMode, "matchMode", "MatchString", "Pattern matching mode: 'FindAllStringSubmatch' or 'MatchString (default)'\nOrganize according to your regex capture groups\n* FindAllStringSubmatch: Finds all matches and submatches (Capture Groups - Yes) - Advisable for Lambda\n* MatchString if any part of the string matches (Capture Groups - No)\n*")
	flag.Parse()
	return cfg
}

func main() {
	cfg := loadConfig()

	patternMatcher, err := pattern.LoadPatterns(cfg.Search)
	if err != nil {
		log.Printf(constants.FailedToLoadPatternsError, err)
		return
	}

	fmt.Printf("Using MatchMode: %s\n", cfg.MatchMode)

	awsCfg, err := loadAWSConfig(cfg)
	if err != nil {
		log.Printf(constants.FailedToLoadAWSConfigError, err)
		return
	}

	selectedServices := parseAndValidateServices(cfg.ServiceFlag)

	if err := processServices(cfg, awsCfg, selectedServices, patternMatcher); err != nil {
		log.Printf(constants.ErrorProcessingServicesError, err)
	}

}

// loadAWSConfig creates and returns an AWS configuration based on the provided Config
func loadAWSConfig(cfg *Config) (aws.Config, error) {
	cfgOpts := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}
	if cfg.Profile != "" {
		cfgOpts = append(cfgOpts, config.WithSharedConfigProfile(cfg.Profile))
	}
	return config.LoadDefaultConfig(context.TODO(), cfgOpts...)
}

// processServices handles the processing of selected AWS services
func processServices(cfg *Config, awsCfg aws.Config, selectedServices map[string]bool, patternMatcher *pattern.Patterns) error {
	if selectedServices[constants.EC2Service] || selectedServices[constants.AllServices] {
		if err := processEC2(cfg, awsCfg, patternMatcher); err != nil {
			return fmt.Errorf("error processing EC2: %w", err)
		}
	}

	if selectedServices[constants.LambdaService] || selectedServices[constants.AllServices] {
		if err := processLambda(cfg, awsCfg, patternMatcher); err != nil {
			return fmt.Errorf("error processing Lambda: %w", err)
		}
	}

	if selectedServices[constants.CloudFormationService] || selectedServices[constants.CloudFormationAlias] || selectedServices[constants.AllServices] {
		if err := processCloudFormation(cfg, awsCfg, patternMatcher); err != nil {
			return fmt.Errorf("error processing CloudFormation: %w", err)
		}
	}

	if selectedServices[constants.SageMakerService] || selectedServices[constants.AllServices] {
		if err := processSageMaker(cfg, awsCfg, patternMatcher); err != nil {
			return fmt.Errorf("error processing SageMaker: %w", err)
		}
	}

	if selectedServices[constants.CodeBuildService] || selectedServices[constants.AllServices] {
		if err := processCodeBuild(cfg, awsCfg, patternMatcher); err != nil {
			return fmt.Errorf("error processing CodeBuild: %w", err)
		}
	}

	if selectedServices[constants.GlueService] || selectedServices[constants.AllServices] {
		if err := processGlue(cfg, awsCfg, patternMatcher); err != nil {
			return fmt.Errorf("error processing Glue: %w", err)
		}
	}

	if selectedServices[constants.EMRService] || selectedServices[constants.AllServices] {
		if err := processEMR(cfg, awsCfg, patternMatcher); err != nil {
			return fmt.Errorf("error processing EMR: %w", err)
		}
	}

	return nil
}

func processCodeBuild(cfg *Config, awsCfg aws.Config, patternMatcher *pattern.Patterns) error {
	fmt.Println("Processing Codebuild projects...")
	codebuildClient := codebuild.NewFromConfig(awsCfg)
	projects, err := services.FetchCodeBuildProjects(context.TODO(), codebuildClient, cfg.Threads)
	if err != nil {
		return fmt.Errorf("error fetching CodeBuild projects: %w", err)
	}

	services.ProcessCodeBuildProjects(projects, patternMatcher, cfg.ShowContent, cfg.MatchMode)
	return nil
}

// processEC2 handles the processing of EC2 instances and launch templates
func processEC2(cfg *Config, awsCfg aws.Config, patternMatcher *pattern.Patterns) error {
	fmt.Println("Processing EC2 Instances and Launch Templates...")
	ec2Client := ec2.NewFromConfig(awsCfg)

	instances, err := services.FetchInstances(context.TODO(), ec2Client, cfg.Threads)
	if err != nil {
		return fmt.Errorf(constants.FailedToFetchInstancesError, err)
	}
	services.ProcessInstances(instances, patternMatcher, cfg.ShowContent, cfg.MatchMode)
	fmt.Println()

	templates, err := services.FetchLaunchTemplates(context.TODO(), ec2Client, cfg.Threads)
	if err != nil {
		return fmt.Errorf(constants.FailedToFetchLaunchTemplatesError, err)
	}
	services.ProcessLaunchTemplates(templates, patternMatcher, cfg.ShowContent, cfg.MatchMode)
	fmt.Println()

	return nil
}

// processLambda handles the processing of Lambda functions
func processLambda(cfg *Config, awsCfg aws.Config, patternMatcher *pattern.Patterns) error {
	fmt.Println("Processing Lambda Functions...")
	lambdaClient := lambda.NewFromConfig(awsCfg)

	lambdaFunctions, err := services.FetchLambdaFunctions(context.TODO(), lambdaClient, cfg.Threads)
	if err != nil {
		return fmt.Errorf(constants.FailedToFetchLambdaFunctionsError, err)
	}
	services.ProcessLambdas(lambdaFunctions, patternMatcher, cfg.ShowContent, cfg.MatchMode)
	fmt.Println()

	return nil
}

// processCloudFormation handles the processing of CloudFormation stacks and stack sets
func processCloudFormation(cfg *Config, awsCfg aws.Config, patternMatcher *pattern.Patterns) error {
	fmt.Println("Processing CloudFormation Stacks and StackSets...")
	cfClient := cloudformation.NewFromConfig(awsCfg)

	stacks, err := services.FetchStacks(context.TODO(), cfClient, cfg.Threads)
	if err != nil {
		return fmt.Errorf(constants.FailedToFetchCloudFormationStacksError, err)
	}

	stackSets, err := services.FetchStackSets(context.TODO(), cfClient, cfg.Threads)
	if err != nil {
		return fmt.Errorf(constants.FailedToFetchCloudFormationStackSetsError, err)
	}

	services.ProcessCloudFormation(stacks, stackSets, patternMatcher, cfg.ShowContent, cfg.MatchMode)
	fmt.Println()

	return nil
}

func processSageMaker(cfg *Config, awsCfg aws.Config, patternMatcher *pattern.Patterns) error {
	fmt.Println("Processing SageMaker Processing Jobs...")
	sageMakerClient := sagemaker.NewFromConfig(awsCfg)

	processingJobs, err := services.FetchSageMakerProcessingJobs(context.TODO(), sageMakerClient, cfg.Threads)
	if err != nil {
		return fmt.Errorf(constants.FailedToFetchSageMakerProcessingJobsError, err)
	}
	services.ProcessSageMakerJobs(processingJobs, patternMatcher, cfg.ShowContent, cfg.MatchMode)
	fmt.Println()

	return nil
}

func processGlue(cfg *Config, awsCfg aws.Config, patternMatcher *pattern.Patterns) error {
	fmt.Println("Processing Glue Jobs...")
	glueClient := glue.NewFromConfig(awsCfg)
	s3Client := s3.NewFromConfig(awsCfg)

	jobs, err := services.FetchGlueJobs(context.TODO(), glueClient, s3Client, cfg.Threads)
	if err != nil {
		return fmt.Errorf("error fetching Glue jobs: %w", err)
	}
	services.ProcessGlueJobs(jobs, patternMatcher, cfg.ShowContent, cfg.MatchMode)
	fmt.Println()

	return nil
}

// processEMR handles the processing of EMR clusters
func processEMR(cfg *Config, awsCfg aws.Config, patternMatcher *pattern.Patterns) error {
	fmt.Println("Processing EMR Clusters...")
	emrClient := emr.NewFromConfig(awsCfg)
	s3Client := s3.NewFromConfig(awsCfg)
	clusters, err := services.FetchEMRClusters(context.TODO(), emrClient, s3Client, cfg.Threads)
	if err != nil {
		return fmt.Errorf("error fetching EMR clusters: %w", err)
	}
	services.ProcessEMRClusters(clusters, patternMatcher, cfg.ShowContent, cfg.MatchMode)
	fmt.Println()
	return nil
}

// parseAndValidateServices parses the service flag and validates the selected services
func parseAndValidateServices(serviceFlag string) map[string]bool {
	servicesMap := make(map[string]bool)
	services := strings.Split(serviceFlag, ",")

	serviceAliases := map[string]string{
		constants.CloudFormationAlias: constants.CloudFormationService,
		constants.LambdaService:       constants.LambdaService,
		constants.EC2Service:          constants.EC2Service,
		constants.SageMakerService:    constants.SageMakerService,
		constants.CodeBuildService:    constants.CodeBuildService,
		constants.GlueService:         constants.GlueService,
		constants.EMRService:          constants.EMRService,
	}

	validServices := map[string]bool{
		constants.EC2Service:            true,
		constants.LambdaService:         true,
		constants.CloudFormationService: true,
		constants.SageMakerService:      true,
		constants.CodeBuildService:      true,
		constants.GlueService:           true,
		constants.EMRService:            true,
		constants.AllServices:           true,
	}

	for _, service := range services {
		service = strings.TrimSpace(strings.ToLower(service))
		if service != "" {
			if actualService, exists := serviceAliases[service]; exists {
				servicesMap[actualService] = true
			} else {
				servicesMap[service] = true
			}
		}
	}

	for service := range servicesMap {
		if !validServices[service] {
			log.Printf("Warning: Unrecognized service '%s' specified in -service flag", service)
		}
	}

	return servicesMap
}
