// discover.go is what the bootstrap finds out from the run's credentials before it asks
// anything: whose they are, which Google Cloud organizations and billing accounts they
// see.

package root

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	resourcemanager "cloud.google.com/go/resourcemanager/apiv3"
	"cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"github.com/go-playground/errors/v5"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/cloudbilling/v1"
	"google.golang.org/api/iterator"
)

// Organization is a Google Cloud organization as the credentials see it: its numeric id,
// its domain (the display name) and its Cloud Identity customer.
type Organization struct {
	ID                  string
	Domain              string
	DirectoryCustomerID string
}

// BillingAccount is a billing account the credentials see.
type BillingAccount struct {
	ID          string
	DisplayName string
	Open        bool
}

// Discovery is what the bootstrap reads about the credentials it runs with.
type Discovery interface {
	// Account is the email of the account the credentials sign as.
	Account(ctx context.Context) (string, error)
	// Organizations lists the organizations the account sees.
	Organizations(ctx context.Context) ([]Organization, error)
	// BillingAccounts lists the billing accounts the account sees.
	BillingAccounts(ctx context.Context) ([]BillingAccount, error)
	Close() error
}

// DiscoveryFunc opens a Discovery.
type DiscoveryFunc func(ctx context.Context) (Discovery, error)

const (
	cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"
	emailScope         = "https://www.googleapis.com/auth/userinfo.email"
	userinfoURL        = "https://www.googleapis.com/oauth2/v3/userinfo"
)

type discovery struct {
	organizations *resourcemanager.OrganizationsClient
	billing       *cloudbilling.APIService
	http          *http.Client
}

// NewDiscovery opens the readers with Application Default Credentials.
func NewDiscovery(ctx context.Context) (Discovery, error) {
	organizations, err := resourcemanager.NewOrganizationsClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "resourcemanager.NewOrganizationsClient()")
	}
	billing, err := cloudbilling.NewService(ctx)
	if err != nil {
		_ = organizations.Close()

		return nil, errors.Wrap(err, "cloudbilling.NewService()")
	}
	client, err := google.DefaultClient(ctx, cloudPlatformScope, emailScope)
	if err != nil {
		_ = organizations.Close()

		return nil, errors.Wrap(err, "google.DefaultClient()")
	}

	return &discovery{organizations: organizations, billing: billing, http: client}, nil
}

func (d *discovery) Account(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userinfoURL, http.NoBody)
	if err != nil {
		return "", errors.Wrap(err, "http.NewRequestWithContext()")
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "asking Google whose credentials these are")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", errors.Wrap(err, "io.ReadAll()")
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.Newf("asking Google whose credentials these are: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var info struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &info); err != nil || info.Email == "" {
		return "", errors.New("Google did not say whose credentials these are: sign in again with gcloud auth application-default login, as the break-glass account")
	}

	return info.Email, nil
}

func (d *discovery) Organizations(ctx context.Context) ([]Organization, error) {
	var orgs []Organization
	it := d.organizations.SearchOrganizations(ctx, &resourcemanagerpb.SearchOrganizationsRequest{})
	for {
		o, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return orgs, nil
		}
		if err != nil {
			return nil, errors.Wrap(err, "resourcemanager.OrganizationsClient.SearchOrganizations()")
		}
		orgs = append(orgs, Organization{ID: strings.TrimPrefix(o.GetName(), "organizations/"), Domain: o.GetDisplayName(), DirectoryCustomerID: o.GetDirectoryCustomerId()})
	}
}

func (d *discovery) BillingAccounts(ctx context.Context) ([]BillingAccount, error) {
	var accounts []BillingAccount
	err := d.billing.BillingAccounts.List().Pages(ctx, func(page *cloudbilling.ListBillingAccountsResponse) error {
		for _, a := range page.BillingAccounts {
			accounts = append(accounts, BillingAccount{ID: strings.TrimPrefix(a.Name, "billingAccounts/"), DisplayName: a.DisplayName, Open: a.Open})
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "cloudbilling.BillingAccountsService.List()")
	}

	return accounts, nil
}

func (d *discovery) Close() error {
	if err := d.organizations.Close(); err != nil {
		return errors.Wrap(err, "resourcemanager.OrganizationsClient.Close()")
	}

	return nil
}
