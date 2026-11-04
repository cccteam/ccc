package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

// TestOrgFound: in a root organization's repository, found writes the record for a
// person to fill in when there is none; with it filled, fills in what the root
// organization supplies, chooses the boot project's suffix, lists the organization in
// placement.json, renders, and says what the merge founds and who grants what before;
// a founded organization is left as it is; a hosting organization founds nothing.
func TestOrgFound(t *testing.T) {
	t.Parallel()

	dir := rootRepo(t)
	if code, out := runOrg(t, "org", "new", dir); code != 0 {
		t.Fatalf("org new: %d\n%s", code, out)
	}
	record := org.FoundingPlacementPath(dir, "rgb")

	code, out := runOrg(t, "org", "found", "rgb", "--dir", dir)
	if code != 0 {
		t.Fatalf("org found: %d\n%s", code, out)
	}
	if !strings.Contains(out, "Wrote "+record+" for you to fill in: appsDomain") {
		t.Errorf("output:\n%s", out)
	}
	h, err := org.ReadPlacementUnchecked(record)
	if err != nil {
		t.Fatal(err)
	}
	if h.Prefix != "rgb" || h.Root == nil || h.Root.Organization != "imr" || h.GithubOrganization != "imp-example" || h.SourceRepo != "rgb-infrastructure" || h.Regions[0].Name != "us-central1" {
		t.Errorf("skeleton = %+v", h)
	}
	root, _ := orgPlacement(dir, "")
	if root.HostingOrganization("rgb").Founded() || len(root.Pending()) != 1 {
		t.Errorf("the listing changed on the skeleton: %v", root.HostingOrganizations)
	}

	// The person fills in what is the organization's own.
	h.AppsDomain, h.Spanner.Config = "apps.rgb.example", "nam10"
	h.Regions[1] = org.Region{Name: "us-west3", Code: "uw3"}
	h.TeamGroups = map[string]string{"tst": "team-tst@imp.example", "stg": "team-stg@imp.example", "prd": "team-prd@imp.example"}
	if err := h.Write(record); err != nil {
		t.Fatal(err)
	}
	code, out = runOrg(t, "org", "found", "rgb", "--dir", dir)
	if code != 0 {
		t.Fatalf("org found again: %d\n%s", code, out)
	}
	filled, err := org.ReadFounding(dir, "rgb")
	if err != nil {
		t.Fatalf("the record does not read as a hosting placement: %v", err)
	}
	root, _ = orgPlacement(dir, "")
	listed := root.HostingOrganization("rgb")
	if listed == nil || listed.BootProject != filled.Projects["boot"] || listed.Founded() {
		t.Errorf("listed = %+v, record's boot project %q", listed, filled.Projects["boot"])
	}
	wantOut := []string{
		"Recorded rgb: boot project " + filled.Projects["boot"] + ", state bucket " + filled.StateBucket + ", repository imp-example/rgb-infrastructure, under the root organization imr.\n",
		"The merge of this pull request founds rgb, as the founding identity (imr-found-gbl-tofu@imr-boot-gbl-core-c3d4.iam.gserviceaccount.com):",
		"a Google Cloud organization administrator grants the founding identity\nFolder Creator (roles/resourcemanager.folderCreator) at the organization node",
		"Billing Account User (roles/billing.user) on 012345-6789AB-CDEF01",
	}
	for _, want := range wantOut {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if code, out := runOrg(t, "org", "check", "--dir", dir); code != 0 {
		t.Errorf("org check after found: %d\n%s", code, out)
	}

	// Founded: the record stays.
	editPlacement(t, dir, func(p *org.Placement) { p.HostingOrganization("rgb").Folder = "900000000001" })
	code, out = runOrg(t, "org", "found", "rgb", "--dir", dir)
	if code != 0 || !strings.Contains(out, "rgb is founded already (folder 900000000001): the record is kept as it stands") {
		t.Errorf("org found of a founded organization: %d\n%s", code, out)
	}

	// A hosting organization founds nothing.
	hosting := fixtureRepo(t, orgFixture)
	if _, err := execute(orgDeps(""), "", "org", "found", "rgb", "--dir", hosting); err == nil || !strings.Contains(err.Error(), "a root organization founds a hosting organization") {
		t.Errorf("org found in a hosting organization: %v", err)
	}
	if _, err := os.Stat(filepath.Join(hosting, org.FoundingsDir)); !os.IsNotExist(err) {
		t.Errorf("a hosting organization got a record directory: %v", err)
	}
}
