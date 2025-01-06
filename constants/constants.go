package constants

const (
	SageMakerService                          = "sagemaker"
	FailedToFetchSageMakerProcessingJobsError = "Failed to fetch SageMaker processing jobs: %v"
	// Services
	EC2Service            = "ec2"
	LambdaService         = "lambda"
	CloudFormationService = "cloudformation"
	CloudFormationAlias   = "cf"
	CodeBuildService      = "codebuild"
	EMRService            = "emr"
	AllServices           = "all"

	// Error messages
	FailedToLoadPatternsError                 = "Failed to load patterns: %v"
	FailedToLoadAWSConfigError                = "Failed to load AWS config: %v"
	ErrorProcessingServicesError              = "Error processing services: %v"
	FailedToFetchInstancesError               = "Failed to fetch instances: %w"
	FailedToFetchLaunchTemplatesError         = "Failed to fetch launch templates: %w"
	FailedToFetchLambdaFunctionsError         = "Failed to fetch Lambda functions: %w"
	FailedToFetchCloudFormationStacksError    = "Failed to fetch CloudFormation stacks: %w"
	FailedToFetchCloudFormationStackSetsError = "Failed to fetch CloudFormation stack sets: %w"
)
