// orgfound.go is bedrock org found: the record of a hosting organization the root
// organization founds by pull request (README.md, "Founding a hosting organization").

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

const foundCommand = "found"

func newOrgFound(d deps) *cobra.Command {
	var dir, placement string
	cmd := &cobra.Command{
		Use:   foundCommand + " <prefix>",
		Short: "Record a hosting organization for the root organization's apply trigger to found",
		Long: `found, in a root organization's repository, records a hosting organization the apply
trigger founds on the merge: the organization's folder at the organization node and the
foundation under it (the terraform folder, the boot project, the boot identity with its roles
on the folder, the state bucket), the first applies of its 0-bootstrap and 1-org with the
projects recorded in its placement, its infrastructure repository with the rendered
foundation as its first push, which its own apply trigger runs on, and the hand-off, a pull
request here that records the folder. Nobody applies by hand (README.md, "Founding a hosting
organization").

The record is the organization's own placement, at organizations/<prefix>/placement.json:
the hosting shape, with what is the organization's to decide written by a person (appsDomain,
spanner.config, two regions, teamGroups, contactDomains, entitlementDurations, labels,
vanityDomains) and what the root organization supplies filled in by this command (the
Google Cloud organization and the billing account, the GitHub organization, its machine
account, default branch and apps, the Cloud Build authorization, the break-glass account, the
bedrock pin, and root, which names this organization). The boot project's suffix is chosen
here too, so its id is known before anything is made. Without the file, found writes one
to fill in and stops. With it, found fills it, lists the organization in placement.json
(hostingOrganizations: its prefix and its boot project; the folder is recorded by the
hand-off), renders, and says what the merge founds and what the administrators grant
before it: the founding identity's Folder Creator at the organization node and Billing
Account User on the billing account, once for every founding, and the new organization's
billing grants after it (its 0-bootstrap/README.md, "Billing grants and the budget").`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return d.orgFound(cmd.Context(), dir, placement, args[0], cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "the root organization's repository root")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the repository root)")

	return cmd
}

// orgFound records the organization: the skeleton when the record is absent, else the
// filled record, the listing, the render and the preview.
func (d deps) orgFound(_ context.Context, dir, placement, prefix string, out io.Writer) error {
	root, err := orgPlacement(dir, placement)
	if err != nil {
		return err
	}
	if !root.IsRoot() {
		return errors.New("this placement is a hosting organization's: a root organization founds a hosting organization (kind root in placement.json)")
	}
	if err := d.orgPinned(root, orgPlacementPath(dir, placement)); err != nil {
		return err
	}
	path := org.FoundingPlacementPath(dir, prefix)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return writeSkeleton(root, prefix, path, out)
	} else if err != nil {
		return errors.Wrapf(err, "os.Stat(): %s", path)
	}
	h, err := readFoundingRecord(path, prefix)
	if err != nil {
		return err
	}
	if err := org.Fill(root, h); err != nil {
		return errors.Wrapf(err, "%s", path)
	}
	if err := h.Write(path); err != nil {
		return err
	}
	fmt.Fprintf(out, "Recorded %s: boot project %s, state bucket %s, repository %s/%s, under the root organization %s.\n", prefix, h.Projects["boot"], h.StateBucket, h.GithubOrganization, h.SourceRepo, root.Prefix)
	changed := root.Record(h)
	if changed {
		if err := root.Write(orgPlacementPath(dir, placement)); err != nil {
			return err
		}
		fmt.Fprintf(out, "Listed %s in placement.json (hostingOrganizations); its folder is recorded by the founding's hand-off.\n", prefix)
	}
	if record := root.HostingOrganization(prefix); record != nil && record.Founded() {
		fmt.Fprintf(out, "%s is founded already (folder %s): the record is kept as it stands, and nothing is founded again.\n", prefix, record.Folder)

		return nil
	}
	files, err := org.Render(root)
	if err != nil {
		return err
	}
	if _, err := org.Write(root, files, dir); err != nil {
		return err
	}
	fmt.Fprint(out, foundingSteps(root, h))

	return nil
}

// readFoundingRecord reads the record as a person wrote it: a hosting placement that
// may lack what the root organization fills in, so it is read without validation and
// validated once filled.
func readFoundingRecord(path, prefix string) (*org.Placement, error) {
	h, err := org.ReadPlacementUnchecked(path)
	if err != nil {
		return nil, err
	}
	if h.Prefix == "" {
		h.Prefix = prefix
	}
	if h.Prefix != prefix {
		return nil, errors.Newf("%s has prefix %q, and the record under %s/%s is that organization's", path, h.Prefix, org.FoundingsDir, prefix)
	}

	return h, nil
}

// writeSkeleton writes the record for a person to fill in, with what the root
// organization supplies already in it and the organization's own values empty.
func writeSkeleton(root *org.Placement, prefix, path string, out io.Writer) error {
	h := &org.Placement{
		Prefix:  prefix,
		Regions: []org.Region{{Name: root.Primary().Name, Code: root.Primary().Code}, {}},
		TeamGroups: map[string]string{
			org.Environments[0]: "", org.Environments[1]: "", org.Environments[2]: "",
		},
		ContactDomains: []string{"@" + root.OrganizationDomain},
		Labels:         root.Labels,
	}
	h.OrganizationDomain, h.OrganizationID, h.BillingAccount = root.OrganizationDomain, root.OrganizationID, root.BillingAccount
	h.GithubOrganization, h.GithubMachineAccount, h.GithubDefaultBranch = root.GithubOrganization, root.GithubMachineAccount, root.GithubDefaultBranch
	h.GithubInfrastructureAppID, h.GithubDeployerAppID, h.GithubReleaseAppID = root.GithubInfrastructureAppID, root.GithubDeployerAppID, root.GithubReleaseAppID
	h.GithubReleaseAppSlug = root.GithubOrganization + "-release"
	h.BreakGlassAccount = root.BreakGlassAccount
	h.SourceRepo = prefix + "-infrastructure"
	h.Root = &org.RootPlace{Organization: root.Prefix, Project: root.Projects["boot"]}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return errors.Wrapf(err, "os.MkdirAll(): %s", filepath.Dir(path))
	}
	if err := h.Write(path); err != nil {
		return err
	}
	fmt.Fprintf(out, `Wrote %s for you to fill in: appsDomain (the domain the applications' names hang under),
spanner.config (the shared instance's configuration, nam10 for example), the second region
(regions, the primary first; the primary is the root organization's, since the Cloud Build
token is read in its region), teamGroups (one Workspace group per environment),
contactDomains, and entitlementDurations, vanityDomains and labels as wanted. Then run
bedrock org found %s again: it fills in what the root organization supplies, chooses the
boot project's suffix and lists the organization in placement.json.
`, path, prefix)

	return nil
}

// foundingSteps says what the merge founds and what is granted by hand before it.
func foundingSteps(root, h *org.Placement) string {
	var b strings.Builder
	fmt.Fprintf(&b, `
The merge of this pull request founds %s, as the founding identity (%s):
  - folder %s at the organization node, the terraform folder under it, the boot project %s
    (its APIs, its billing link to %s), the boot identity %s-boot-gbl-tofu with its roles on
    the folder, the break-glass account's Folder Admin on the folder, and the state bucket
    %s;
  - the first applies of its 0-bootstrap and 1-org, against that bucket: its connection from
    the Cloud Build authorization this organization recorded, its repository link and its
    two triggers; its folders, its six projects with their identities, recorded in its
    placement (projects, projectNumbers);
  - the repository %s/%s, private, with the rendered foundation as its first push, which
    its apply trigger runs on, every layer as its own identity;
  - the founding identity's own grants off everything it made, a marker in this
    organization's state bucket, and the hand-off pull request here, which records the
    folder (placement.json, hostingOrganizations) and the placement as founded.
Before the merge, by hand, once per root organization (0-bootstrap/README.md, "The founding
identity's grants"): a Google Cloud organization administrator grants the founding identity
Folder Creator (roles/resourcemanager.folderCreator) at the organization node, and a billing
administrator grants it Billing Account User (roles/billing.user) on %s. After the merge, for
the new organization: the same billing administrator grants its boot and org identities
Billing Account User, which 1-org needs when it links a project it creates (that repository's
0-bootstrap/README.md, "Billing grants and the budget"), and sets its spend budget; its
consent screen and role groups follow as its README says.
`, h.Prefix, root.FoundingIdentity(), h.Prefix, h.Projects["boot"], h.BillingAccount, h.Prefix, h.StateBucket, h.GithubOrganization, h.SourceRepo, h.BillingAccount)

	return b.String()
}
