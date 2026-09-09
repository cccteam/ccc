package rpc

import (
	"context"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

type (
	// CompileBriefing is the CLIENT-FORM method: its Execute takes a resource.Client
	// instead of a transaction, so it runs outside one. It reads the sector's missions AS
	// THE CALLER (the query is armed with Enforce(caller)), so the Marshal's briefing counts
	// every mission with its fee, the Dispatcher's the live ones, and the Archivist's the
	// closed ones with a redaction counted wherever her grant masks the fee, with no
	// masking code in the body; and it asks, as data, whether the caller may see the hazard
	// board before folding the worst readings in. It answers a typed briefing the console
	// renders as a printed sheet. A dry run is refused with 400: there is no transaction to
	// roll back. The Execute grant is row-free (no @target) and held by the Sector Marshal,
	// the Dispatcher, and the Archivist.
	//
	// Demonstrates: rpc.client-form, rpc.armed-read, rpc.decision-as-data, rpc.typed-result, rpc.row-free.
	//
	// @rpc
	// @permissionScope(domain)
	CompileBriefing struct {
		// IncludeHazards asks for the hazard board's worst readings beside the missions.
		IncludeHazards bool
	}

	// Briefing is the sheet: how many missions the caller may see and how many are still
	// open, the worst hazard among them, the fees the caller may see and how many were
	// redacted, the overdue missions by identifier, and the hazard readings when they were
	// asked for and the caller may read them.
	Briefing struct {
		Sector          string
		CompiledAt      time.Time
		Missions        int64
		OpenMissions    int64
		WorstHazard     int64
		FeesOutstanding decimal.Decimal
		FeesRedacted    int64
		Overdue         []ccc.UUID
		HazardBoard     []HazardLine
		HazardWithheld  bool
	}

	// HazardLine is one ship's worst reading on one subsystem.
	HazardLine struct {
		ShipName     string
		Subsystem    string
		WorstReading float64
	}
)

// sectorHazardBoards is the computed resource the briefing asks about before reading
// telemetry.
const sectorHazardBoards accesstypes.Resource = "SectorHazardBoards"

// Execute runs outside a transaction, against the client, as the caller.
func (m *CompileBriefing) Execute(ctx context.Context, client resource.Client, _ *Client) (*Briefing, error) {
	caller := resource.CallerFrom(ctx)
	sector, ok := caller.Scope.Domain()
	if !ok {
		return nil, errors.New("CompileBriefing is sector-scoped but was checked in the global scope")
	}
	now := time.Now().UTC()
	briefing := &Briefing{Sector: string(sector), CompiledAt: now, FeesOutstanding: decimal.Zero}

	// The armed read marks a masked fee on the row envelope by its wire name, the same
	// way the generated list handler does, so a redaction is read from the row itself.
	missions := resources.NewMissionQuery().
		AddColumns(resources.NewMissionColumns().ID().Hazard().Fee().Deadline().StatusID()).
		Where(resources.NewMissionQueryClause().SectorID().Equal(string(sector))).
		Enforce(caller)
	for row, err := range missions.List(ctx, client) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.MissionQuery.List()")
		}
		briefing.Missions++
		briefing.WorstHazard = max(briefing.WorstHazard, row.Data.Hazard)
		if row.Masked("fee") {
			briefing.FeesRedacted++
		} else {
			briefing.FeesOutstanding = briefing.FeesOutstanding.Add(row.Data.Fee)
		}
		switch resources.MissionStatus(row.Data.StatusID) {
		case resources.CompletedMissionStatus, resources.FailedMissionStatus, resources.StoodDownMissionStatus:
			continue
		default:
		}
		briefing.OpenMissions++
		if row.Data.Deadline.Before(now) {
			briefing.Overdue = append(briefing.Overdue, row.Data.ID)
		}
	}

	if !m.IncludeHazards {
		return briefing, nil
	}
	decisions, err := caller.Check(ctx, accesstypes.List, sectorHazardBoards)
	if err != nil {
		return nil, errors.Wrap(err, "resource.Caller.Check()")
	}
	if !decisions[sectorHazardBoards].IsGranted() {
		briefing.HazardWithheld = true

		return briefing, nil
	}
	lines, err := worstReadings(ctx, client, sector)
	if err != nil {
		return nil, err
	}
	briefing.HazardBoard = lines

	return briefing, nil
}

// worstReadings folds the sector's telemetry to the worst reading per ship and subsystem,
// the same fold the hazard board renders.
func worstReadings(ctx context.Context, client resource.Client, sector accesstypes.Domain) ([]HazardLine, error) {
	names := make(map[ccc.UUID]string)
	for row, err := range resources.NewShipQuery().AddColumns(resources.NewShipColumns().ID().Name()).List(ctx, client) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.ShipQuery.List()")
		}
		names[row.Data.ID] = row.Data.Name
	}
	worst := make(map[[2]string]*HazardLine)
	var order [][2]string
	reports := resources.NewDroidReportQuery().
		AddColumns(resources.NewDroidReportColumns().All()).
		Where(resources.NewDroidReportQueryClause().SectorID().Equal(string(sector)))
	for row, err := range reports.List(ctx, client) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.DroidReportQuery.List()")
		}
		key := [2]string{row.Data.ShipID.String(), row.Data.Subsystem}
		line, ok := worst[key]
		if !ok {
			line = &HazardLine{ShipName: names[row.Data.ShipID], Subsystem: row.Data.Subsystem}
			worst[key] = line
			order = append(order, key)
		}
		line.WorstReading = max(line.WorstReading, row.Data.Reading)
	}
	lines := make([]HazardLine, 0, len(order))
	for _, key := range order {
		lines = append(lines, *worst[key])
	}

	return lines, nil
}
