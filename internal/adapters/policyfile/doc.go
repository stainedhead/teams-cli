// Package policyfile loads the trusted, strictly validated teams policy file
// (FR-21, FR-22) and exposes it as a usecase.PolicyProvider. A policy that
// fails trust or validation never loads; there is no fallback policy.
package policyfile
