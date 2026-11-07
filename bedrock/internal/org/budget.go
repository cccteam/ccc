// budget.go is the spend budget a placement names: a hosting organization's on its own
// folder, made by the root organization's creation, and a root organization's on the
// whole billing account, made by the bootstrap; both kept by the root organization's apply,
// with alerts at half, nine tenths and the whole of the amount to the billing account's
// administrators and users, which is whom Cloud Billing mails when a budget names no
// channel. A budget sends alerts and stops nothing.

package org

import (
	"math"
	"slices"
	"strings"
)

// Budget is one spend budget on the billing account, as Cloud Billing holds it and the
// apply keeps it.
type Budget struct {
	// Name is the budget's resource name (billingAccounts/B/budgets/ID), empty on one
	// not made yet.
	Name string
	// DisplayName names the budget in the console and in its alerts; the apply tells the
	// budgets it keeps by it.
	DisplayName string
	// Units is the monthly amount in whole units of the billing account's currency, which
	// CurrencyCode names on a budget read back.
	Units        int64
	CurrencyCode string
	// Ancestors are the folders the budget counts (folders/F); none counts every project
	// on the billing account.
	Ancestors []string
	// Thresholds are the fractions of the amount at which an alert is sent, on actual
	// spend.
	Thresholds []float64
}

// BudgetThresholds are the alerts every budget here carries: half, nine tenths and the
// whole of the amount.
var BudgetThresholds = []float64{0.5, 0.9, 1}

// HostingBudgetName names a hosting organization's budget, the one on its folder.
func HostingBudgetName(prefix string) string {
	return prefix + " organization"
}

// TotalBudgetName names a root organization's budget, the one on the whole billing
// account.
func TotalBudgetName(prefix string) string {
	return prefix + " billing account"
}

// HostingBudget is a hosting organization's budget: the amount its record names, on its
// own folder.
func HostingBudget(prefix string, units int64, folder string) Budget {
	return Budget{DisplayName: HostingBudgetName(prefix), Units: units, Ancestors: []string{"folders/" + folder}, Thresholds: slices.Clone(BudgetThresholds)}
}

// TotalBudget is a root organization's budget: the amount its placement names, on every
// project of the billing account.
func TotalBudget(prefix string, units int64) Budget {
	return Budget{DisplayName: TotalBudgetName(prefix), Units: units, Thresholds: slices.Clone(BudgetThresholds)}
}

// ExportDataset is the dataset in the root project the billing account's export lands in:
// every charge by project, service, SKU and label, from which cost is reported by
// application, environment or organization. Google has no API for pointing an account's
// export at a dataset: a person does, once per billing account, in the Billing console.
const ExportDataset = "billing"

// ExportLocation is where the export's dataset is made: the multi-region of the primary
// region's continent where Cloud Billing supports one (US, EU), else the primary region
// itself, which Cloud Billing's export supports for some regions and refuses for others
// when the export is pointed at the dataset. A dataset's location cannot change.
func (p *Placement) ExportLocation() string {
	region := p.Primary().Name
	switch {
	case strings.HasPrefix(region, "us-"):
		return "US"
	case strings.HasPrefix(region, "europe-"):
		return "EU"
	default:
		return region
	}
}

// Same reports whether the other budget says the same amount, counts the same folders
// and alerts at the same thresholds.
func (b Budget) Same(o Budget) bool {
	if b.Units != o.Units || !sameSet(b.Ancestors, o.Ancestors) || len(b.Thresholds) != len(o.Thresholds) {
		return false
	}
	mine, theirs := slices.Clone(b.Thresholds), slices.Clone(o.Thresholds)
	slices.Sort(mine)
	slices.Sort(theirs)
	for i := range mine {
		if math.Abs(mine[i]-theirs[i]) > 1e-9 {
			return false
		}
	}

	return true
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)

	return slices.Equal(a, b)
}
