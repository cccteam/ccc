// billing.go is what the root organization keeps on the billing account and for its
// reporting: the spend budgets, its own on the whole account and one per hosting
// organization on its folder, and the dataset in the root project the billing export
// lands in, whose tables say whether the export was pointed at it and still lands.

package root

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

// The two sections, as the report names them.
const (
	sectionBudgets       = "budgets"
	sectionBillingExport = "billing export"
)

// exportStale is how long a usage cost table may go unwritten before the export is
// reported stopped: Cloud Billing writes the tables several times a day, and an account
// with projects is charged every day.
const exportStale = 3 * 24 * time.Hour

// ExportTables are the tables the three exports make in the dataset for the billing
// account, in the order of exportNames: the standard usage cost export's, the detailed
// usage cost export's and the pricing export's. The usage tables are written daily; the
// pricing table when prices change, so its age says nothing.
func ExportTables(billingAccount string) []string {
	id := strings.ReplaceAll(billingAccount, "-", "_")

	return []string{"gcp_billing_export_v1_" + id, "gcp_billing_export_resource_v1_" + id, "cloud_pricing_export"}
}

// exportNames name the three exports as the Billing console does, in ExportTables' order;
// usageTables is how many of them are usage cost exports, whose tables are written daily.
var exportNames = []string{"standard usage cost export", "detailed usage cost export", "pricing export"}

const usageTables = 2

// budgetsSection makes the spend budgets the billing account lacks and brings one that
// differs back: the root organization's own on every project of the account, and one
// per hosting organization created, on its folder, with the amount its record names.
// A budget is told by its display name; its amount, its folders and its thresholds are
// compared. A run refused the read of the account's budgets (a plan trigger's, whose
// runner holds nothing on the billing account) notes it and goes on.
func (e *engine) budgetsSection(ctx context.Context) error {
	want := []org.Budget{e.d.TotalBudget()}
	for _, record := range e.p.HostingOrganizations {
		h, ok, err := e.hostingRecord(record)
		if err != nil {
			return err
		}
		folder := e.organizationFolder(record)
		if !ok || folder == "" {
			continue
		}
		want = append(want, org.HostingBudget(h.Prefix, h.MonthlyBudget, folder))
	}
	got, err := e.budgets.Budgets(ctx, e.p.BillingAccount)
	if err != nil {
		if e.req.Bootstrap || status.Code(errors.Cause(err)) != codes.PermissionDenied {
			return err
		}
		e.note("the spend budgets on billing account " + e.p.BillingAccount + " were not compared: this run holds no read of the billing account, as the triggers' runners hold their roles under the organization's folder alone; the apply keeps them as the creating identity, and so does bedrock org check run by a Billing Account Administrator")

		return nil
	}
	for _, w := range want {
		i := slices.IndexFunc(got, func(b org.Budget) bool { return b.DisplayName == w.DisplayName })
		switch {
		case i < 0:
			if err := e.change(ctx, sectionBudgets, fmt.Sprintf("makes the spend budget %q on billing account %s: %d a month in the account's currency %s, with alerts at 50, 90 and 100 percent of it to the account's administrators and users", w.DisplayName, e.p.BillingAccount, w.Units, counting(w)), func(ctx context.Context) error {
				b := w

				return e.budgets.CreateBudget(ctx, e.p.BillingAccount, &b)
			}); err != nil {
				return err
			}
		case !got[i].Same(w):
			w.Name = got[i].Name
			if err := e.change(ctx, sectionBudgets, fmt.Sprintf("brings the spend budget %q on billing account %s back to %d a month %s, with alerts at 50, 90 and 100 percent", w.DisplayName, e.p.BillingAccount, w.Units, counting(w)), func(ctx context.Context) error {
				b := w

				return e.budgets.UpdateBudget(ctx, &b)
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

// counting says what a budget counts: every project of the account, or its folders.
func counting(b org.Budget) string {
	if len(b.Ancestors) == 0 {
		return "on every project of the account"
	}
	folders := make([]string, 0, len(b.Ancestors))
	for _, a := range b.Ancestors {
		folders = append(folders, strings.TrimPrefix(a, "folders/"))
	}

	return "on folder " + strings.Join(folders, ", ")
}

// billingExport makes the dataset the billing export lands in when the root project
// lacks it, and otherwise looks for the three exports' tables: one missing, or a usage
// table unwritten for exportStale, is a finding, since the export is pointed at the
// dataset by hand, once per billing account, and nothing else would say it was not, or
// was turned off. A dataset made this run has nothing to look for yet.
func (e *engine) billingExport(ctx context.Context) error {
	project := e.d.Project()
	_, exists, err := e.datasets.Dataset(ctx, project, org.ExportDataset)
	if err != nil {
		return err
	}
	if !exists {
		return e.change(ctx, sectionBillingExport, fmt.Sprintf("makes the dataset %s in %s (%s), where the billing export lands", org.ExportDataset, project, e.p.ExportLocation()), func(ctx context.Context) error {
			return e.datasets.CreateDataset(ctx, project, org.ExportDataset, e.p.ExportLocation(), e.d.Labels())
		})
	}
	tables, err := e.datasets.Tables(ctx, project, org.ExportDataset)
	if err != nil {
		return err
	}
	where := project + "." + org.ExportDataset
	now := e.now()
	for i, want := range ExportTables(e.p.BillingAccount) {
		j := slices.IndexFunc(tables, func(t TableInfo) bool { return t.ID == want })
		switch {
		case j < 0:
			e.finding(fmt.Sprintf("the billing export has not landed in %s: the table %s is not there; in the Billing console, a person points the %s of billing account %s at the dataset, once (README.md, \"Spend budgets and the billing export\"), and the table appears within hours, the pricing export's within two days", where, want, exportNames[i], e.p.BillingAccount))
		case i < usageTables && now.Sub(tables[j].LastModified) > exportStale:
			e.finding(fmt.Sprintf("the billing export to %s stopped: the table %s was last written %d days ago; in the Billing console, the %s of billing account %s should still point at the dataset (README.md, \"Spend budgets and the billing export\")", where, want, int(now.Sub(tables[j].LastModified).Hours()/24), exportNames[i], e.p.BillingAccount))
		}
	}

	return nil
}

// now is the run's clock: the clients' when a test sets one.
func (e *engine) now() time.Time {
	if e.clients.Now != nil {
		return e.clients.Now()
	}

	return time.Now()
}
