// destroy.go is bedrock org destroy for a root organization: a person deletes the root
// project, and with it the secrets, the zones, the connection, the triggers, the
// identities and the state bucket, takes the creating identity's grants off the
// organization node and the billing account, and deletes the folder. The repository
// stays on GitHub, with its code and pull requests, and the apps stay with their
// installations: a bootstrap again brings them as brought apps, with their keys, or
// makes new ones.

package root

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

// DestroyRequest is one destroy: the placement; the folder is found by its name.
type DestroyRequest struct {
	Placement *org.Placement
}

// CanDestroy is why the root organization cannot be destroyed yet, or nil: a hosting
// organization recorded as created (hostingOrganizations with a folder) is destroyed
// first, in its own repository, and its entry taken out.
func CanDestroy(p *org.Placement) error {
	if !p.IsRoot() {
		return errors.Newf("the %s organization is a hosting organization: bedrock org destroy destroys it through its layers", p.Prefix)
	}
	var created []string
	for _, h := range p.CreatedHostingOrganizations() {
		created = append(created, h.Prefix)
	}
	if len(created) > 0 {
		return errors.Newf("placement.json records %s as created hosting organizations (hostingOrganizations with a folder): destroy each in its own repository first (bedrock org destroy there), then take its entry out and destroy the root organization", strings.Join(created, ", "))
	}

	return nil
}

// Destroy deletes the root organization.
func Destroy(ctx context.Context, clients *Clients, req *DestroyRequest, out io.Writer) error {
	p := req.Placement
	if err := CanDestroy(p); err != nil {
		return err
	}
	google, err := clients.Google(ctx)
	if err != nil {
		return err
	}
	defer google.Close()
	iam, err := clients.IAM(ctx)
	if err != nil {
		return err
	}
	defer iam.Close()
	d := NewDesired(p)
	for _, g := range d.OrganizationGrants() {
		if err := iam.RevokeOrganization(ctx, p.OrganizationID, g.Member, []string{g.Role}); err != nil {
			return err
		}
	}
	for _, g := range d.BillingGrants() {
		if err := iam.RevokeBilling(ctx, p.BillingAccount, g.Member, []string{g.Role}); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "Took the creating identity's and the runners' grants off the organization node, and the creating identity's off billing account %s.\n", p.BillingAccount)
	project := p.Projects[org.RootProjectKey]
	if project != "" {
		exists, err := google.ProjectExists(ctx, project)
		if err != nil {
			return err
		}
		if exists {
			if err := iam.DeleteProject(ctx, project); err != nil {
				return err
			}
			fmt.Fprintf(out, "Deleted the project %s, with the secrets, the zones, the connection, the triggers, the identities and the state bucket: Google holds it for 30 days, and its id never returns.\n", project)
		} else {
			fmt.Fprintf(out, "The project %s is gone already.\n", project)
		}
	}
	folder, err := google.FindFolder(ctx, "organizations/"+p.OrganizationID, p.Prefix)
	if err != nil {
		return err
	}
	if folder != "" {
		if err := iam.DeleteFolder(ctx, folder); err != nil {
			return errors.Wrapf(err, "the folder %s refuses to go while a project under it is being deleted; run bedrock org destroy again in a little while", folder)
		}
		fmt.Fprintf(out, "Deleted the folder %s (%s).\n", p.Prefix, folder)
	}

	return nil
}
