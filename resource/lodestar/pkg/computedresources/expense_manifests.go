package computedresources

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"iter"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

type (
	// ExpenseManifest is the purser's per-mission rollup of the expenses booked against
	// a mission's sorties: how many sorties flew and what they cost. It is the RENDERED
	// FILE: the struct-scope @file declares that each manifest has a document, and the
	// generator serves it at GET .../expense-manifests/{missionId}/content by calling
	// ExpenseManifestContent below, which renders the booked expenses as a text/csv
	// sheet at request time. The gate is Read on ExpenseManifests and a Read grant on
	// content, the route's own field; the Purser holds both, the marshal neither. The
	// sheet's validator is a digest of its rows, so a browser that kept the sheet asks
	// again and hears 304 until an expense is booked.
	//
	// Demonstrates: @file.rendered, @computed, computed.domain.
	//
	// @computed
	// @permissionScope(domain)
	// @file
	// @order(Title asc)
	// @page(default: 25, max: 200)
	ExpenseManifest struct {
		MissionID ccc.UUID        `spanner:"MissionId"` // @primarykey
		Title     string          `spanner:"Title"`
		Sorties   int64           `spanner:"Sorties"`
		Expenses  decimal.Decimal `spanner:"Expenses"`
	}
)

// Resource implements resource.Resourcer.
func (ExpenseManifest) Resource() accesstypes.Resource {
	return "ExpenseManifests"
}

// ListExpenseManifest yields one manifest per mission in the sector; the generated
// handler applies the request's filter, sort, and page over them.
func ListExpenseManifest(ctx context.Context, qSet *resource.QuerySet[ExpenseManifest], client resource.Client, _ *Client) iter.Seq2[*ExpenseManifest, error] {
	return func(yield func(*ExpenseManifest, error) bool) {
		sector, err := sectorOf(qSet)
		if err != nil {
			yield(nil, err)

			return
		}
		manifests, err := manifestsOf(ctx, client, sector, nil)
		if err != nil {
			yield(nil, err)

			return
		}
		for _, manifest := range manifests {
			if !yield(manifest, nil) {
				return
			}
		}
	}
}

// ReadExpenseManifest answers one mission's manifest; nil when the mission is not the
// sector's.
func ReadExpenseManifest(ctx context.Context, missionID ccc.UUID, qSet *resource.QuerySet[ExpenseManifest], client resource.Client, _ *Client) (*ExpenseManifest, error) {
	sector, err := sectorOf(qSet)
	if err != nil {
		return nil, err
	}
	manifests, err := manifestsOf(ctx, client, sector, &missionID)
	if err != nil {
		return nil, err
	}
	for _, manifest := range manifests {
		return manifest, nil
	}

	return nil, nil
}

// ExpenseManifestContent renders one mission's manifest as a text/csv sheet, one line
// per booked expense: the content function the struct-scope @file names, called by the
// generated route after its gate. It returns nil for a mission that is not the sector's,
// which the frame answers 404 as the read route would, and it never touches the response:
// the frame writes the headers, answers 304 when the request's validator matches the
// sheet's digest, and copies the bytes.
//
// Demonstrates: @file.rendered.
func ExpenseManifestContent(ctx context.Context, missionID ccc.UUID, qSet *resource.QuerySet[ExpenseManifest], client resource.Client, _ *Client) (*resource.Content, error) {
	sector, err := sectorOf(qSet)
	if err != nil {
		return nil, err
	}
	manifests, err := manifestsOf(ctx, client, sector, &missionID)
	if err != nil {
		return nil, err
	}
	if len(manifests) == 0 {
		return nil, nil
	}

	var sheet bytes.Buffer
	w := csv.NewWriter(&sheet)
	if err := w.Write([]string{"sortie", "pilot", "launchedAt", "category", "amount", "note"}); err != nil {
		return nil, errors.Wrap(err, "csv.Writer.Write()")
	}
	sorties := resources.NewSortieQuery().
		AddColumns(resources.NewSortieColumns().ID().PilotUserID().LaunchedAt()).
		Where(resources.NewSortieQueryClause().MissionID().Equal(missionID))
	for sortie, err := range sorties.List(ctx, client) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.SortieQuery.List()")
		}
		expenses := resources.NewSortieExpenseQuery().
			AddColumns(resources.NewSortieExpenseColumns().Category().Amount().Note()).
			Where(resources.NewSortieExpenseQueryClause().SortieID().Equal(sortie.Data.ID))
		for expense, err := range expenses.List(ctx, client) {
			if err != nil {
				return nil, errors.Wrap(err, "resources.SortieExpenseQuery.List()")
			}
			note := ""
			if expense.Data.Note != nil {
				note = *expense.Data.Note
			}
			line := []string{sortie.Data.ID.String(), sortie.Data.PilotUserID, sortie.Data.LaunchedAt.UTC().Format("2006-01-02T15:04:05Z"), expense.Data.Category, expense.Data.Amount.String(), note}
			if err := w.Write(line); err != nil {
				return nil, errors.Wrap(err, "csv.Writer.Write()")
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, errors.Wrap(err, "csv.Writer.Flush()")
	}

	// The validator is the sheet's own digest: a kept copy revalidates for free until
	// an expense is booked.
	digest := sha256.Sum256(sheet.Bytes())

	return &resource.Content{
		Name:        fmt.Sprintf("manifest-%s.csv", missionID),
		ContentType: "text/csv",
		Size:        int64(sheet.Len()),
		Tag:         hex.EncodeToString(digest[:8]),
		Body:        io.NopCloser(bytes.NewReader(sheet.Bytes())),
	}, nil
}

// manifestsOf folds the sector's missions, their sorties, and the expenses booked
// against them into one manifest per mission, narrowed to one mission when asked. Rows
// come back in the missions' title order; the generated handler sorts and pages them.
func manifestsOf(ctx context.Context, client resource.Client, sector accesstypes.Domain, missionID *ccc.UUID) ([]*ExpenseManifest, error) {
	missions := resources.NewMissionQuery().
		AddColumns(resources.NewMissionColumns().ID().Title()).
		Where(resources.NewMissionQueryClause().SectorID().Equal(string(sector)))
	if missionID != nil {
		missions = resources.NewMissionQuery().
			AddColumns(resources.NewMissionColumns().ID().Title()).
			Where(resources.NewMissionQueryClause().SectorID().Equal(string(sector)).And().ID().Equal(*missionID))
	}

	var manifests []*ExpenseManifest
	for mission, err := range missions.List(ctx, client) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.MissionQuery.List()")
		}
		manifest := &ExpenseManifest{MissionID: mission.Data.ID, Title: mission.Data.Title}
		sorties := resources.NewSortieQuery().
			AddColumns(resources.NewSortieColumns().ID()).
			Where(resources.NewSortieQueryClause().MissionID().Equal(mission.Data.ID))
		for sortie, err := range sorties.List(ctx, client) {
			if err != nil {
				return nil, errors.Wrap(err, "resources.SortieQuery.List()")
			}
			manifest.Sorties++
			expenses := resources.NewSortieExpenseQuery().
				AddColumns(resources.NewSortieExpenseColumns().Amount()).
				Where(resources.NewSortieExpenseQueryClause().SortieID().Equal(sortie.Data.ID))
			for expense, err := range expenses.List(ctx, client) {
				if err != nil {
					return nil, errors.Wrap(err, "resources.SortieExpenseQuery.List()")
				}
				manifest.Expenses = manifest.Expenses.Add(expense.Data.Amount)
			}
		}
		manifests = append(manifests, manifest)
	}

	return manifests, nil
}
