// policies.go is the organization-policy baseline the root organization sets on each
// hosting organization's folders. Google grants Organization Policy Administrator at the
// organization node and nowhere lower, and nothing of a hosting organization holds a role
// there (the root README, "Several organizations"), so the hosting organization's 1-org
// makes the folders and the root organization's apply, whose applier holds the role (and
// whose planner holds Organization Policy Viewer, both granted by the bootstrap), sets the
// policies on them: in the creation, right after 1-org's first apply, and on every apply
// after, from the organization's record (organizations/<prefix>/placement.json), which
// the creation filled with the folders' ids and each environment project's public-invoker
// tag (folders, publicInvokerTags).

package root

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

// The two constraints set beside the baseline list, each with a shape of its own.
const (
	memberDomainsConstraint  = "iam.allowedPolicyMemberDomains"
	contactDomainsConstraint = "essentialcontacts.managed.allowedContactDomains"
)

// policyFolders are the folders 1-org makes under the organization's folder, by key, in
// the order the policies are set: the terraform folder is left out, so the boot project
// and the layer identities in it are never locked out by a constraint they depend on.
var policyFolders = []string{"shared", "tst", "stg", "prd"}

// BaselinePolicy is one constraint of the baseline and the rule set on every folder.
type BaselinePolicy struct {
	Constraint string
	Rule       PolicyRule
}

func enforced() PolicyRule {
	t := true

	return PolicyRule{Enforce: &t}
}

// Baseline is the constraints set on every folder of every hosting organization, the
// tf-gcp-setup baseline trimmed to constraints that cost nothing operationally: each
// closes a door nothing here uses (service account keys, VM serial consoles, external
// IPs, HMAC keys) or pins a decision already made (one load balancer type, Cloud Run
// behind it, GitHub as the only Cloud Build source).
var Baseline = []BaselinePolicy{
	// Service account credentials: keys are the one long-lived credential a service
	// account can have, nothing here needs one, and every runner authenticates as its
	// service account directly.
	{Constraint: "iam.managed.disableServiceAccountKeyCreation", Rule: enforced()},
	{Constraint: "iam.managed.disableServiceAccountKeyUpload", Rule: enforced()},
	{Constraint: "iam.serviceAccountKeyExpiryHours", Rule: PolicyRule{AllowedValues: []string{"2160h"}}},
	{Constraint: "iam.allowServiceAccountCredentialLifetimeExtension", Rule: PolicyRule{DenyAll: true}},
	{Constraint: "iam.automaticIamGrantsForDefaultServiceAccounts", Rule: enforced()},
	{Constraint: "iam.disableAuditLoggingExemption", Rule: enforced()},
	// Cloud Storage: every bucket is uniform-access, private, and reached over TLS with
	// an IAM identity rather than an HMAC key.
	{Constraint: "storage.publicAccessPrevention", Rule: enforced()},
	{Constraint: "storage.secureHttpTransport", Rule: enforced()},
	{Constraint: "storage.uniformBucketLevelAccess", Rule: enforced()},
	{Constraint: "storage.restrictAuthTypes", Rule: PolicyRule{DeniedValues: []string{"in:ALL_HMAC_SIGNED_REQUESTS"}}},
	// Compute: no VMs are planned, so these cost nothing; they keep any VM that does
	// appear off the internet and off the serial console.
	{Constraint: "compute.disableGlobalSerialPortAccess", Rule: enforced()},
	{Constraint: "compute.disableSerialPortAccess", Rule: enforced()},
	{Constraint: "compute.requireOsLogin", Rule: enforced()},
	{Constraint: "compute.requireShieldedVm", Rule: enforced()},
	{Constraint: "compute.skipDefaultNetworkCreation", Rule: enforced()},
	{Constraint: "compute.vmCanIpForward", Rule: PolicyRule{DenyAll: true}},
	{Constraint: "compute.vmExternalIpAccess", Rule: PolicyRule{DenyAll: true}},
	// Ingress: only the global external managed HTTP(S) load balancer, so every request
	// path runs through the same TLS and Cloud Armor configuration, and Cloud Run is
	// reachable only through it or from inside the VPC.
	{Constraint: "compute.restrictLoadBalancerCreationForTypes", Rule: PolicyRule{AllowedValues: []string{"GLOBAL_EXTERNAL_MANAGED_HTTP_HTTPS"}}},
	{Constraint: "run.allowedIngress", Rule: PolicyRule{AllowedValues: []string{"internal-and-cloud-load-balancing"}}},
	{Constraint: "gcp.restrictTLSVersion", Rule: PolicyRule{DeniedValues: []string{"TLS_VERSION_1", "TLS_VERSION_1_1"}}},
	// Cloud Build: builds run as an explicit service account (the legacy default one is
	// never created), and source connections are GitHub only.
	{Constraint: "cloudbuild.disableCreateDefaultServiceAccount", Rule: enforced()},
	{Constraint: "cloudbuild.allowedIntegrations", Rule: PolicyRule{AllowedValues: []string{"github.com"}}},
}

// FolderPolicy is one policy to set: the folder's key and id, the constraint, and the
// policy itself.
type FolderPolicy struct {
	FolderKey  string
	FolderID   string
	Constraint string
	Policy     Policy
}

// What names the policy for a line of the report.
func (f FolderPolicy) What(prefix string) string {
	return fmt.Sprintf("%s on folder %s (%s) of %s", f.Constraint, f.FolderKey, f.FolderID, prefix)
}

// DesiredPolicies are the policies of one hosting organization, from its record: the
// baseline on each of its four folders; on each environment folder, domain-restricted
// sharing to the Google Cloud organization's own customer with the public-invoker
// exception, a Cloud Run service tagged with that environment project's public-invoker
// tag, which may name allUsers; and on each folder the allowed contact domains, the
// record's. A folder or a tag the record lacks is a finding, and that folder's policies
// wait for it.
func DesiredPolicies(h *org.Placement) (policies []FolderPolicy, findings []string) {
	for _, key := range policyFolders {
		id := h.Folders[key]
		if id == "" {
			findings = append(findings, fmt.Sprintf("the record of %s names no folder %s (folders), so no policy is set on it: the creation records the folders from 1-org's first apply; an organization created before that recorded them has them written by hand (gcloud resource-manager folders list --folder <organization folder>)", h.Prefix, key))

			continue
		}
		name := func(constraint string) string { return "folders/" + id + "/policies/" + constraint }
		for _, b := range Baseline {
			policies = append(policies, FolderPolicy{FolderKey: key, FolderID: id, Constraint: b.Constraint, Policy: Policy{Name: name(b.Constraint), Rules: []PolicyRule{b.Rule}}})
		}
		if slices.Contains(org.Environments, key) {
			tag, ok := h.PublicInvokerTags[key]
			if !ok {
				findings = append(findings, fmt.Sprintf("the record of %s names no public-invoker tag for %s (publicInvokerTags), so domain-restricted sharing is not set on folder %s: the creation records the tags from 1-org's first apply (its outputs public_invoker_tag_key and public_invoker_tag_value)", h.Prefix, key, key))
			} else {
				policies = append(policies, FolderPolicy{FolderKey: key, FolderID: id, Constraint: memberDomainsConstraint, Policy: Policy{Name: name(memberDomainsConstraint), Rules: []PolicyRule{
					{Condition: &Condition{Title: "public-invoker", Description: "A Cloud Run service the application layer tagged for the load balancer.", Expression: fmt.Sprintf("resource.matchTagId('%s', '%s')", tag.Key, tag.Value)}, AllowAll: true},
					{AllowedValues: []string{h.DirectoryCustomerID}},
				}}})
			}
		}
		parameters, _ := json.Marshal(map[string]any{"allowedDomains": h.ContactDomains})
		policies = append(policies, FolderPolicy{FolderKey: key, FolderID: id, Constraint: contactDomainsConstraint, Policy: Policy{Name: name(contactDomainsConstraint), Rules: []PolicyRule{{Enforce: enforced().Enforce, Parameters: string(parameters)}}}})
	}

	return policies, findings
}

// RulesEqual reports whether two policies' rules say the same: rule for rule, the same
// shape, the same values (in any order), the same condition, and the same parameters
// (as JSON, in any key order).
func RulesEqual(got, want []PolicyRule) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if !ruleEqual(got[i], want[i]) {
			return false
		}
	}

	return true
}

func ruleEqual(a, b PolicyRule) bool {
	if (a.Enforce == nil) != (b.Enforce == nil) || (a.Enforce != nil && *a.Enforce != *b.Enforce) {
		return false
	}
	if a.DenyAll != b.DenyAll || a.AllowAll != b.AllowAll || !sameSet(a.AllowedValues, b.AllowedValues) || !sameSet(a.DeniedValues, b.DeniedValues) {
		return false
	}
	if (a.Condition == nil) != (b.Condition == nil) || (a.Condition != nil && *a.Condition != *b.Condition) {
		return false
	}

	return sameJSON(a.Parameters, b.Parameters)
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)

	return slices.Equal(a, b)
}

func sameJSON(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return a == b
	}

	return reflect.DeepEqual(x, y)
}

// orgPolicies is the section: for each hosting organization created (recorded with its
// folder, or created by this run), each policy of the baseline it lacks or holds
// otherwise is set.
func (e *engine) orgPolicies(ctx context.Context) error {
	for _, record := range e.p.HostingOrganizations {
		h, ok, err := e.hostingRecord(record)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		policies, findings := DesiredPolicies(h)
		for _, f := range findings {
			e.finding(f)
		}
		for _, want := range policies {
			got, exists, err := e.policies.Policy(ctx, want.Policy.Name)
			if err != nil {
				return err
			}
			switch {
			case !exists:
				if err := e.change(ctx, sectionPolicies, "sets "+want.What(h.Prefix), func(ctx context.Context) error {
					return e.policies.CreatePolicy(ctx, "folders/"+want.FolderID, &want.Policy)
				}); err != nil {
					return err
				}
			case !RulesEqual(got.Rules, want.Policy.Rules):
				if err := e.change(ctx, sectionPolicies, "brings "+want.What(h.Prefix)+" to the baseline", func(ctx context.Context) error {
					return e.policies.UpdatePolicy(ctx, &want.Policy)
				}); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// hostingRecord is the hosting organization's placement the policies are set from: the
// one this run's creation recorded, else the record in the repository of an organization
// recorded with its folder; an organization not created yet has none (ok false).
func (e *engine) hostingRecord(record org.HostingOrganization) (*org.Placement, bool, error) {
	if h, ok := e.created[record.Prefix]; ok {
		return h, true, nil
	}
	if !record.Created() {
		return nil, false, nil
	}
	h, err := org.ReadRecord(e.req.Dir, record.Prefix)
	if err != nil {
		return nil, false, errors.Wrapf(err, "the record of %s, from which the root organization sets its organization policies", record.Prefix)
	}

	return h, true, nil
}
