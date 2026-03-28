package bootstrap

import testconstants "github.com/ffreis/platform-bootstrap/internal/test"

// Shared test constants for all internal/bootstrap package tests.
const (
	testRegion       = testconstants.RegionUSEast1
	testCallerARN    = "arn:aws:iam::123:root"
	errUnexpectedFmt = "unexpected error: %v"
)
