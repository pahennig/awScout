# awScout
Security-focused tool that scans multiple AWS services for exposed secrets and sensitive data using regex-based detection

This repository helps developers and security teams identify credentials, tokens, and other confidential information stored across multiple AWS services.



### Supported Services
- EC2
    * User Data
    * Launch Templates with versioning
- CloudFormation
    * Stack
    * StackSets
    * Parameters
- Lambda
    * Code with versioning
    * Environment variables
- Glue
    * Jobs
    * Scripts stored in S3
- Codebuild
    * Source/Buildspec
    * Environment Variables
- Sagemaker
    * Jobs
- EMR
    * Bootstrap actions
    * Scripts

### How-to use
1. Ensure GO is installed
2. `git clone https://github.com/pahennig/awScout.git`
3. `cd awScout`
5. Choose the supported services (ec2, cloudformation, lambda, glue, codebuild, sagemaker, emr) and run like the example below
4. `go run cmd/main.go -profile $aws-profile -search pattern/findallstring.json  -service ec2,cloudformation --show`

### Example
With and without offuscation
![Demo of usage](./img/record.gif)

### Required AWS Permissions
The policy below includes permissions for all services and actions required to fully run the script. However, if you prefer to limit the scope to specific services, you can customize the policy accordingly. For example, to run it only for EC2, you would only need the following permissions: `ec2:DescribeInstances`, `ec2:DescribeLaunchTemplates`, `ec2:DescribeInstanceAttribute` and `ec2:DescribeLaunchTemplateVersions`:
```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "VisualEditor0",
            "Effect": "Allow",
            "Action": "s3:GetObject",
            "Resource": "arn:aws:s3:::*/*"
        },
        {
            "Sid": "VisualEditor1",
            "Effect": "Allow",
            "Action": [
                "codebuild:BatchGetProjects",
                "cloudformation:ListStacks",
                "ec2:DescribeInstances",
                "lambda:ListFunctions",
                "elasticmapreduce:ListBootstrapActions",
                "ec2:DescribeLaunchTemplates",
                "lambda:ListVersionsByFunction",
                "ec2:DescribeInstanceAttribute",
                "lambda:GetFunction",
                "elasticmapreduce:ListSteps",
                "ec2:DescribeLaunchTemplateVersions",
                "glue:ListJobs",
                "cloudformation:DescribeStacks",
                "sagemaker:ListProcessingJobs",
                "sagemaker:DescribeProcessingJob",
                "cloudformation:DescribeStackSet",
                "cloudformation:ListStackSets",
                "cloudformation:GetTemplate",
                "elasticmapreduce:ListClusters",
                "codebuild:ListProjects",
                "glue:GetJob",
                "elasticmapreduce:ListBootstrapActions",
                "elasticmapreduce:ListSteps",
                "elasticmapreduce:ListClusters"
            ],
            "Resource": "*"
        }
    ]
}
```