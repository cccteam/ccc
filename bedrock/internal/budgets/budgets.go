// Package budgets is the Cloud Billing Budgets API as bedrock uses it: the budgets of one
// billing account, listed, made and brought to the amount a placement names (org.Budget).
// The creation opens it as the creating identity, a Billing Account Administrator, and the
// root organization's engine as the run's credentials or as that identity.
package budgets

import (
	"context"
	"net/http"
	"slices"

	"github.com/go-playground/errors/v5"
	"google.golang.org/api/billingbudgets/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

// spendBasis is the spend a threshold is measured against: what was actually spent.
const spendBasis = "CURRENT_SPEND"

// Client reads, makes and updates the budgets of a billing account.
type Client interface {
	// Budgets is every budget on the billing account (its id, 012345-6789AB-CDEF01).
	Budgets(ctx context.Context, billingAccount string) ([]org.Budget, error)
	// CreateBudget makes the budget on the billing account and fills in its name.
	CreateBudget(ctx context.Context, billingAccount string, b *org.Budget) error
	// UpdateBudget brings the budget's amount, folders and thresholds to b's, by its name.
	UpdateBudget(ctx context.Context, b *org.Budget) error
	// DeleteBudget deletes the budget by its name; one gone already is no error.
	DeleteBudget(ctx context.Context, name string) error
	// Close releases the connection.
	Close() error
}

type client struct {
	svc *billingbudgets.Service
}

// New opens the Budgets API with the process's credentials and the options given (a token
// source for an identity, a quota project for a person's credentials).
func New(ctx context.Context, opts ...option.ClientOption) (Client, error) {
	svc, err := billingbudgets.NewService(ctx, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "billingbudgets.NewService()")
	}

	return &client{svc: svc}, nil
}

func (c *client) Budgets(ctx context.Context, billingAccount string) ([]org.Budget, error) {
	var budgets []org.Budget
	err := c.svc.BillingAccounts.Budgets.List("billingAccounts/"+billingAccount).Pages(ctx, func(page *billingbudgets.GoogleCloudBillingBudgetsV1ListBudgetsResponse) error {
		for _, b := range page.Budgets {
			budgets = append(budgets, fromAPI(b))
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "billingbudgets.BillingAccounts.Budgets.List(): %s", billingAccount)
	}

	return budgets, nil
}

func (c *client) CreateBudget(ctx context.Context, billingAccount string, b *org.Budget) error {
	made, err := c.svc.BillingAccounts.Budgets.Create("billingAccounts/"+billingAccount, toAPI(b)).Context(ctx).Do()
	if err != nil {
		return errors.Wrapf(err, "billingbudgets.BillingAccounts.Budgets.Create(): %q on %s", b.DisplayName, billingAccount)
	}
	b.Name, b.CurrencyCode = made.Name, fromAPI(made).CurrencyCode

	return nil
}

func (c *client) UpdateBudget(ctx context.Context, b *org.Budget) error {
	if _, err := c.svc.BillingAccounts.Budgets.Patch(b.Name, toAPI(b)).UpdateMask("amount,budgetFilter,thresholdRules").Context(ctx).Do(); err != nil {
		return errors.Wrapf(err, "billingbudgets.BillingAccounts.Budgets.Patch(): %s", b.Name)
	}

	return nil
}

func (c *client) DeleteBudget(ctx context.Context, name string) error {
	_, err := c.svc.BillingAccounts.Budgets.Delete(name).Context(ctx).Do()
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound {
		return nil
	}
	if err != nil {
		return errors.Wrapf(err, "billingbudgets.BillingAccounts.Budgets.Delete(): %s", name)
	}

	return nil
}

// DeleteNamed deletes the budget of the display name on the billing account, which is how
// bedrock tells the budgets it made, and reports whether one stood. The destroys take
// their own budget off this way: the root organization's (org.TotalBudgetName) and a
// hosting organization's (org.HostingBudgetName).
func DeleteNamed(ctx context.Context, c Client, billingAccount, displayName string) (bool, error) {
	got, err := c.Budgets(ctx, billingAccount)
	if err != nil {
		return false, err
	}
	i := slices.IndexFunc(got, func(b org.Budget) bool { return b.DisplayName == displayName })
	if i < 0 {
		return false, nil
	}
	if err := c.DeleteBudget(ctx, got[i].Name); err != nil {
		return false, err
	}

	return true, nil
}

// Close has nothing to release: the REST client holds no connection of its own.
func (*client) Close() error {
	return nil
}

// fromAPI is the budget as org.Budget reads it: the amount's units and currency, the
// folders it counts, and the thresholds on actual spend.
func fromAPI(b *billingbudgets.GoogleCloudBillingBudgetsV1Budget) org.Budget {
	out := org.Budget{Name: b.Name, DisplayName: b.DisplayName}
	if b.Amount != nil && b.Amount.SpecifiedAmount != nil {
		out.Units, out.CurrencyCode = b.Amount.SpecifiedAmount.Units, b.Amount.SpecifiedAmount.CurrencyCode
	}
	if b.BudgetFilter != nil {
		out.Ancestors = b.BudgetFilter.ResourceAncestors
	}
	for _, rule := range b.ThresholdRules {
		if rule.SpendBasis == "" || rule.SpendBasis == spendBasis {
			out.Thresholds = append(out.Thresholds, rule.ThresholdPercent)
		}
	}

	return out
}

// toAPI is the budget as the API takes it: the amount in the billing account's currency
// (left unnamed, which the API reads as the account's), the folders as the filter, no
// filter for every project on the account, and a rule per threshold on actual spend. No
// notification rule is set, so the alerts go to the billing account's administrators and
// users.
func toAPI(b *org.Budget) *billingbudgets.GoogleCloudBillingBudgetsV1Budget {
	out := &billingbudgets.GoogleCloudBillingBudgetsV1Budget{
		DisplayName: b.DisplayName,
		Amount:      &billingbudgets.GoogleCloudBillingBudgetsV1BudgetAmount{SpecifiedAmount: &billingbudgets.GoogleTypeMoney{Units: b.Units}},
	}
	if len(b.Ancestors) > 0 {
		out.BudgetFilter = &billingbudgets.GoogleCloudBillingBudgetsV1Filter{ResourceAncestors: b.Ancestors}
	}
	for _, t := range b.Thresholds {
		out.ThresholdRules = append(out.ThresholdRules, &billingbudgets.GoogleCloudBillingBudgetsV1ThresholdRule{ThresholdPercent: t, SpendBasis: spendBasis})
	}

	return out
}
