package bootstrap

type ResourceType string

const (
	ResourceTypeDynamoDBTable ResourceType = "DynamoDBTable"
	ResourceTypeS3Bucket      ResourceType = "S3Bucket"
	ResourceTypeIAMRole       ResourceType = "IAMRole"
	ResourceTypeIAMUser       ResourceType = "IAMUser"
	ResourceTypeSNSTopic      ResourceType = "SNSTopic"
	ResourceTypeAWSBudget     ResourceType = "AWSBudget"
)
