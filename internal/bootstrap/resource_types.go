package bootstrap

type ResourceType string

const (
	ResourceTypeDynamoDBTable ResourceType = "DynamoDBTable"
	ResourceTypeS3Bucket      ResourceType = "S3Bucket"
	ResourceTypeIAMRole       ResourceType = "IAMRole"
	ResourceTypeSNSTopic      ResourceType = "SNSTopic"
	ResourceTypeAWSBudget     ResourceType = "AWSBudget"
)
