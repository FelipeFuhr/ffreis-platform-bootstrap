package aws

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	budgetstypes "github.com/aws/aws-sdk-go-v2/service/budgets/types"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
)

func TestDeleteStateBucket_NotFoundIsNil(t *testing.T) {
	m := &mockS3{bucketExists: false}

	if err := DeleteStateBucket(context.Background(), m, "missing"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.deleteBucketCalls != 0 {
		t.Errorf("deleteBucketCalls: want 0, got %d", m.deleteBucketCalls)
	}
}

func TestDeleteStateBucket_HeadBucketUnexpectedError(t *testing.T) {
	m := &mockS3{headErr: errors.New("boom")}

	err := DeleteStateBucket(context.Background(), m, "state")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "checking bucket state before delete") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteStateBucket_DeletesAfterEmptying(t *testing.T) {
	m := &mockS3{
		bucketExists:          true,
		listObjectVersionsSeq: []*s3.ListObjectVersionsOutput{{}},
	}

	if err := DeleteStateBucket(context.Background(), m, "state"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.deleteBucketCalls != 1 {
		t.Errorf("deleteBucketCalls: want 1, got %d", m.deleteBucketCalls)
	}
}

func TestDeleteStateBucket_DeleteObjectsAPIErr(t *testing.T) {
	m := &mockS3{
		bucketExists: true,
		listObjectVersionsSeq: []*s3.ListObjectVersionsOutput{{
			Versions: []s3types.ObjectVersion{{Key: sdkaws.String("k1"), VersionId: sdkaws.String("v1")}},
		}},
		deleteObjectsErr: errors.New("delete failed"),
	}

	err := DeleteStateBucket(context.Background(), m, "state")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "batch deleting objects") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteStateBucket_DeleteObjectsOutputErrors(t *testing.T) {
	m := &mockS3{
		bucketExists: true,
		listObjectVersionsSeq: []*s3.ListObjectVersionsOutput{{
			Versions: []s3types.ObjectVersion{{Key: sdkaws.String("k1"), VersionId: sdkaws.String("v1")}},
		}},
		deleteObjectsOut: &s3.DeleteObjectsOutput{
			Errors: []s3types.Error{{
				Key:     sdkaws.String("k1"),
				Message: sdkaws.String("access denied"),
			}},
		},
	}

	err := DeleteStateBucket(context.Background(), m, "state")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "deleting objects") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteStateBucket_PaginatesUntilNotTruncated(t *testing.T) {
	m := &mockS3{
		bucketExists: true,
		listObjectVersionsSeq: []*s3.ListObjectVersionsOutput{
			{
				Versions:            []s3types.ObjectVersion{{Key: sdkaws.String("k1"), VersionId: sdkaws.String("v1")}},
				IsTruncated:         sdkaws.Bool(true),
				NextKeyMarker:       sdkaws.String("next-key"),
				NextVersionIdMarker: sdkaws.String("next-ver"),
			},
			{
				DeleteMarkers: []s3types.DeleteMarkerEntry{{Key: sdkaws.String("k2"), VersionId: sdkaws.String("v2")}},
				IsTruncated:   sdkaws.Bool(false),
			},
		},
	}

	if err := DeleteStateBucket(context.Background(), m, "state"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.listObjectVersionsCalls != 2 {
		t.Errorf("listObjectVersionsCalls: want 2, got %d", m.listObjectVersionsCalls)
	}
	if m.deleteObjectsCalls != 2 {
		t.Errorf("deleteObjectsCalls: want 2, got %d", m.deleteObjectsCalls)
	}
}

func TestDeleteDynamoDBTable_NotFoundIsNil(t *testing.T) {
	m := &mockDynamoDB{deleteErr: &dbtypes.ResourceNotFoundException{}}

	if err := DeleteDynamoDBTable(context.Background(), m, "missing"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteDynamoDBTable_UnexpectedError(t *testing.T) {
	m := &mockDynamoDB{deleteErr: errors.New("boom")}

	err := DeleteDynamoDBTable(context.Background(), m, "tbl")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "deleting DynamoDB table tbl") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteIAMRole_NotFoundIsNil(t *testing.T) {
	m := &mockIAM{roleExists: false}

	if err := DeleteIAMRole(context.Background(), m, "missing-role"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteIAMRole_CheckRoleError(t *testing.T) {
	m := &mockIAM{getRoleErr: errors.New("boom")}

	err := DeleteIAMRole(context.Background(), m, "role")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "checking IAM role role before delete") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteIAMRole_ListPoliciesError(t *testing.T) {
	m := &mockIAM{roleExists: true, listPoliciesErr: errors.New("boom")}

	err := DeleteIAMRole(context.Background(), m, "role")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "listing inline policies for role role") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteIAMRole_DeletePolicyError(t *testing.T) {
	m := &mockIAM{
		roleExists:      true,
		policyNames:     []string{"p1"},
		deletePolicyErr: errors.New("boom"),
	}

	err := DeleteIAMRole(context.Background(), m, "role")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "deleting inline policy p1 from role role") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteIAMRole_DeleteRoleError(t *testing.T) {
	m := &mockIAM{
		roleExists:    true,
		deleteRoleErr: errors.New("boom"),
	}

	err := DeleteIAMRole(context.Background(), m, "role")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "deleting IAM role role") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteIAMRole_SuccessDeletesInlinePoliciesThenRole(t *testing.T) {
	m := &mockIAM{
		roleExists:  true,
		policyNames: []string{"p1", "p2"},
	}

	if err := DeleteIAMRole(context.Background(), m, "role"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.roleExists {
		t.Error("roleExists: want false after delete, got true")
	}
}

func TestDeleteSNSTopic_NotFoundIsNil(t *testing.T) {
	m := &mockSNS{deleteErr: &snstypes.NotFoundException{}}

	if err := DeleteSNSTopic(context.Background(), m, "us-east-1", testAccountID, "missing-topic"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteSNSTopic_UnexpectedError(t *testing.T) {
	m := &mockSNS{deleteErr: errors.New("boom")}

	err := DeleteSNSTopic(context.Background(), m, "us-east-1", testAccountID, "topic")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "deleting SNS topic") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteBudget_NotFoundIsNil(t *testing.T) {
	m := &mockBudgets{deleteErr: &budgetstypes.NotFoundException{}}

	if err := DeleteBudget(context.Background(), m, testAccountID, testBudgetName); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteBudget_UnexpectedError(t *testing.T) {
	m := &mockBudgets{deleteErr: errors.New("boom")}

	err := DeleteBudget(context.Background(), m, testAccountID, testBudgetName)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "deleting budget") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteStateBucket_NotFoundErrorTypeMatch(t *testing.T) {
	// This is a regression guard: DeleteStateBucket relies on errors.As
	// matching *s3types.NotFound.
	m := &mockS3{headErr: &s3types.NotFound{}}

	if err := DeleteStateBucket(context.Background(), m, "missing"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteIAMRole_NotFoundErrorTypeMatch(t *testing.T) {
	// This is a regression guard: DeleteIAMRole treats NoSuchEntity as "already gone".
	m := &mockIAM{getRoleErr: &iamtypes.NoSuchEntityException{}}

	if err := DeleteIAMRole(context.Background(), m, "missing-role"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
