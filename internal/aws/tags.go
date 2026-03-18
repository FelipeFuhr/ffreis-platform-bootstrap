package aws

// RequiredTags returns the mandatory tags that must be applied to every
// platform-managed resource. These tags are non-negotiable: any bootstrap
// step that creates a resource must apply them and must fail if tagging fails.
//
// Tag semantics:
//   - Project: constant "platform" — identifies the workload family.
//   - Layer: constant "bootstrap" — identifies the infrastructure layer.
//   - Owner: the org name — identifies the owning team or product.
func RequiredTags(orgName string) map[string]string {
	return map[string]string{
		"Project": "platform",
		"Layer":   "bootstrap",
		"Owner":   orgName,
	}
}
