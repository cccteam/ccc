package generation

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Test_queryTables_shapes runs the walk over the query shapes applications write. Each
// case keeps a real query's structure, its joins, nesting, clause order and the places
// where names collide, under the fixture's own vocabulary, and says the schema it
// assumes and the set the walk must derive from it, or the refusal it must raise.
func Test_queryTables_shapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tables  []string
		sql     string
		want    []string
		wantErr string
	}{
		{
			name:   "two joins, every column qualified by its table, a trailing comma before FROM",
			tables: []string{"Deckhands", "Crews", "Voyages"},
			sql: `SELECT
  Deckhands.Id,
  Deckhands.CrewId,
  Deckhands.Rank,
  Crews.VoyageId,
  Voyages.Label,
FROM Deckhands
JOIN Crews ON Deckhands.CrewId = Crews.Id
JOIN Voyages ON Crews.VoyageId = Voyages.Id
`,
			want: []string{"Crews", "Deckhands", "Voyages"},
		},
		{
			name:   "a left join, an aliased column, a trailing comma",
			tables: []string{"Crews", "Tolls"},
			sql: `SELECT
  Crews.Id,
  Crews.VoyageId,
  Crews.RateId,
  Tolls.FlatRate AS FixedToll,
FROM Crews
LEFT JOIN Tolls ON Crews.RateId = Tolls.Id
`,
			want: []string{"Crews", "Tolls"},
		},
		{
			name:   "a derived table over a derived table over three unions, each grouped, aliases without AS, a WHERE of ORs and an ORDER BY after the outer alias",
			tables: []string{"Voyages", "Arrivals", "Departures"},
			sql: `SELECT
  Tally.GroupedOn AS GroupedOn,
  Tally.ChaseCount AS ChaseCount,
  Tally.HeldCount AS HeldCount,
  (ChaseCount + HeldCount) AS AllCount,
FROM (
	SELECT
		GroupedOn,
		SUM(ChaseCount) AS ChaseCount,
		SUM(HeldCount) AS HeldCount
	FROM (
		SELECT
			v.ScheduledOn AS GroupedOn,
			SUM(CASE WHEN v.Status IN ('tide','slack') AND v.Reviewer IS NOT NULL THEN 1 ELSE 0 END) AS ChaseCount,
			SUM(CASE WHEN v.Status = 'slack' AND v.Reviewer IS NULL THEN 1 ELSE 0 END) AS HeldCount
		FROM Voyages v
		WHERE v.ScheduledOn IS NOT NULL
		GROUP BY v.ScheduledOn

		UNION ALL

		SELECT
			a.ReceivedOn AS GroupedOn,
			SUM(CASE WHEN a.Status = 'tide' AND a.Reviewer IS NOT NULL THEN 1 ELSE 0 END) AS ChaseCount,
			0 AS HeldCount
		FROM Arrivals a
		WHERE a.ReceivedOn IS NOT NULL
		GROUP BY a.ReceivedOn

		UNION ALL

		SELECT
			d.ReceivedOn AS GroupedOn,
			SUM(CASE WHEN d.Status = 'tide' AND d.Reviewer IS NOT NULL THEN 1 ELSE 0 END) AS ChaseCount,
			0 AS HeldCount
		FROM Departures d
		WHERE d.ReceivedOn IS NOT NULL
		GROUP BY d.ReceivedOn
	) Pooled
	GROUP BY GroupedOn
) Tally
WHERE
Tally.ChaseCount > 0
OR Tally.HeldCount > 0
ORDER BY Tally.GroupedOn DESC
`,
			want: []string{"Arrivals", "Departures", "Voyages"},
		},
		{
			name:   "two left joins, COALESCE over both, columns named Type and Status",
			tables: []string{"Voyages", "VoyageMasters", "Skippers"},
			sql: `SELECT
  Voyages.Id,
  Voyages.Type,
  Voyages.Status,
  Voyages.ReceivedOn,
  COALESCE(Skippers.Callsign, VoyageMasters.Callsign) AS MasterCallsign,
  VoyageMasters.SkipperId AS MasterSkipperId,
FROM Voyages
LEFT JOIN VoyageMasters ON Voyages.Id = VoyageMasters.Id
LEFT JOIN Skippers ON VoyageMasters.SkipperId = Skippers.Id
`,
			want: []string{"Skippers", "VoyageMasters", "Voyages"},
		},
		{
			name:   "a grouped derived table joined between two tables, its alias referenced in another case, a parenthesized column",
			tables: []string{"ManifestLines", "Manifests", "Cargoes"},
			sql: `SELECT
  Manifests.Id AS ManifestId,
  ManifestLines.LoggedOn,
  (firstDates.FirstLoadedOn) AS AbsoluteFirstLoadedOn,
  ManifestLines.Status,
  Cargoes.OwnerSkipperId
FROM ManifestLines
JOIN Manifests ON Manifests.Id = ManifestLines.ManifestId
JOIN (SELECT l.CargoId, MIN(l.FirstLoadedOn) AS FirstLoadedOn FROM ManifestLines l GROUP BY CargoId) AS firstDates ON firstdates.cargoId = ManifestLines.CargoId
JOIN Cargoes ON Cargoes.Id = ManifestLines.CargoId
`,
			want: []string{"Cargoes", "ManifestLines", "Manifests"},
		},
		{
			name:   "three whole queries under UNION ALL, parameters in an IN list and as a select-list value aliased Type",
			tables: []string{"Voyages", "VoyageMasters", "Skippers", "Arrivals", "Departures"},
			sql: `SELECT
  Voyages.Id,
  Voyages.Type,
  Voyages.ScheduledOn AS GroupedOn,
  Voyages.Reviewer,
  Voyages.Status,
  COALESCE(Skippers.Callsign, VoyageMasters.Callsign) AS MasterCallsign
FROM Voyages
LEFT JOIN VoyageMasters ON Voyages.Id = VoyageMasters.Id
LEFT JOIN Skippers ON VoyageMasters.SkipperId = Skippers.Id
WHERE Voyages.Status IN (@cleared, @held)
  AND Voyages.Reviewer IS NOT NULL

UNION ALL

SELECT
  Arrivals.Id,
  @arrival AS Type,
  Arrivals.ReceivedOn AS GroupedOn,
  Arrivals.Reviewer,
  Arrivals.Status,
  COALESCE(Skippers.Callsign, VoyageMasters.Callsign) AS MasterCallsign
FROM Arrivals
JOIN Voyages ON Arrivals.VoyageId = Voyages.Id
LEFT JOIN VoyageMasters ON Voyages.Id = VoyageMasters.Id
LEFT JOIN Skippers ON VoyageMasters.SkipperId = Skippers.Id
WHERE Arrivals.Status IN (@cleared)
  AND Arrivals.Reviewer IS NOT NULL

UNION ALL

SELECT
  Departures.Id,
  Departures.Type,
  Departures.ReceivedOn AS GroupedOn,
  Departures.Reviewer,
  Departures.Status,
  Skippers.Callsign AS MasterCallsign
FROM Departures
JOIN Skippers ON Departures.SkipperId = Skippers.Id
WHERE Departures.Status IN (@cleared)
  AND Departures.Reviewer IS NOT NULL
`,
			want: []string{"Arrivals", "Departures", "Skippers", "VoyageMasters", "Voyages"},
		},
		{
			name:   "a grouped derived table left-joined right after the FROM table, a trailing space after it",
			tables: []string{"Cargoes", "Loadings"},
			sql: `SELECT
  Cargoes.Id,
  Cargoes.Status,
  (firstDates.FirstLoadedOn) AS AbsoluteFirstLoadedOn,
FROM Cargoes 
LEFT JOIN (SELECT Loadings.CargoId, MIN(Loadings.LoadedOn) AS FirstLoadedOn FROM Loadings GROUP BY CargoId) AS firstDates ON firstdates.cargoId = Cargoes.Id
`,
			want: []string{"Cargoes", "Loadings"},
		},
		{
			name:   "a left join, a multi-line grouped derived table joined on two conditions, then two inner joins",
			tables: []string{"Skippers", "Moorings", "CargoSkippers", "Cargoes"},
			sql: `SELECT
  Skippers.Id AS SkipperId,
  Skippers.Callsign,
  Moorings.Pier,
  Moorings.Slot,
  CargoSkippers.Duty AS Duty,
  Cargoes.Id AS CargoId,
FROM Skippers
LEFT JOIN Moorings ON Moorings.SkipperId = Skippers.Id
LEFT JOIN (
	SELECT m.SkipperId, MAX(m.EffectiveOn) as EffectiveOn FROM Moorings AS m GROUP BY m.SkipperId
) AS LatestMooring ON LatestMooring.SkipperId = Moorings.SkipperId AND LatestMooring.EffectiveOn = Moorings.EffectiveOn
JOIN CargoSkippers ON CargoSkippers.SkipperId = Skippers.Id
JOIN Cargoes ON Cargoes.Id = CargoSkippers.CargoId
`,
			want: []string{"CargoSkippers", "Cargoes", "Moorings", "Skippers"},
		},
		{
			name:   "a derived table holding a UNION DISTINCT of two grouped queries, the first over a four-way join nested as a left-joined derived table, DATE calls, BETWEEN, an IN list, HAVING",
			tables: []string{"Cargoes", "Skippers", "Soundings", "Haulages", "HaulageLines", "Receipts", "Portions"},
			sql: `SELECT
  Cargoes.OwnerSkipperId,
  Cargoes.Id AS CargoId,
  Cargoes.Marking,
  (Depths.TakenOn) AS SalvagedOn,
  (Depths.Reading) AS SalvageAmount,
  Cargoes.Salvaged,
FROM Cargoes
JOIN Skippers ON Skippers.Id = Cargoes.OwnerSkipperId
JOIN (
	SELECT
		Soundings.CargoId,
		Soundings.TakenOn,
		SUM(Soundings.Reading)*-1 as Reading
	FROM Soundings
	LEFT JOIN (
		SELECT
			Haulages.CargoId
		FROM Haulages
		JOIN HaulageLines ON HaulageLines.HaulageId = Haulages.Id
		JOIN Receipts ON Receipts.Id = HaulageLines.ReceiptId
		JOIN Portions ON Portions.HaulageLineId = HaulageLines.Id
		WHERE Receipts.Code = 'salvage'
		GROUP BY Haulages.CargoId
	) AS LedgerSalvage ON LedgerSalvage.CargoId = Soundings.CargoId
	WHERE
		Soundings.Code = 'salvage'
		AND Soundings.TakenOn >= DATE('2001-02-03')
	GROUP BY
		Soundings.CargoId,
		Soundings.TakenOn

	UNION DISTINCT

	SELECT
		Haulages.CargoId,
		MAX(Receipts.TakenOn) as TakenOn,
		SUM(Portions.Amount)*-1 as Reading
	FROM Haulages
	JOIN HaulageLines ON HaulageLines.HaulageId = Haulages.Id
	JOIN Receipts ON Receipts.Id = HaulageLines.ReceiptId
	JOIN Portions ON Portions.HaulageLineId = HaulageLines.Id
	WHERE Receipts.Code IN ('salvage', 'wreck', 'tow', 'salvage1', 'wreck1', 'tow1')
		AND Portions.Kind BETWEEN 'a' AND 'z'
		AND Receipts.TakenOn >= DATE('2001-02-03')
	GROUP BY Haulages.CargoId
) AS Depths ON Depths.CargoId = Cargoes.Id
WHERE Cargoes.Region IN ('north', 'south')
GROUP BY
	Cargoes.OwnerSkipperId,
	Cargoes.Id,
	Cargoes.marking,
	Cargoes.Region,
	Depths.TakenOn,
	Depths.Reading,
	Cargoes.Salvaged
HAVING Depths.Reading >= .01
`,
			want: []string{"Cargoes", "HaulageLines", "Haulages", "Portions", "Receipts", "Skippers", "Soundings"},
		},
		{
			name:   "three joins and a WHERE against a parameter, tabs and trailing spaces in the select list",
			tables: []string{"Apprentices", "Cargoes", "CargoSkippers", "Skippers"},
			sql: `SELECT
	Apprentices.Id,
	Apprentices.Grade, 
	Apprentices.Standing,
	Cargoes.Id as CargoId, 
	Cargoes.Marking,
	Skippers.Id as SkipperId
FROM Cargoes
JOIN CargoSkippers ON CargoSkippers.CargoId = Cargoes.Id
JOIN Skippers ON Skippers.Id = CargoSkippers.SkipperId
JOIN Apprentices ON Apprentices.SkipperId = Skippers.Id
WHERE CargoSkippers.Duty = @duty
`,
			want: []string{"Apprentices", "CargoSkippers", "Cargoes", "Skippers"},
		},
		{
			name:   "three left joins, arithmetic in the select list, COALESCE",
			tables: []string{"Arrivals", "Voyages", "VoyageMasters", "Skippers"},
			sql: `SELECT
  Arrivals.Id,
  Arrivals.VoyageId,
  Arrivals.ReceivedOn,
  Arrivals.BaseAsked + Arrivals.ExtraAsked AS TotalAsked,
  Arrivals.Status,
  Voyages.Marking,
  COALESCE(Skippers.Callsign, VoyageMasters.Callsign) AS MasterCallsign,
FROM Arrivals
LEFT JOIN Voyages ON Arrivals.VoyageId = Voyages.Id
LEFT JOIN VoyageMasters ON Voyages.Id = VoyageMasters.Id
LEFT JOIN Skippers ON VoyageMasters.SkipperId = Skippers.Id
`,
			want: []string{"Arrivals", "Skippers", "VoyageMasters", "Voyages"},
		},
		{
			name:   "two whole queries under UNION ALL, one a join with a parameter WHERE, the other three left joins, a parameter aliased Type",
			tables: []string{"Departures", "Skippers", "Arrivals", "Voyages", "VoyageMasters"},
			sql: `SELECT
  Departures.Id,
  Departures.Type,
  Departures.ReceivedOn,
  Departures.Reviewer,
  Departures.Status,
  Skippers.Callsign AS MasterCallsign
FROM Departures
JOIN Skippers ON Departures.SkipperId = Skippers.Id
WHERE Departures.Status = @state
  AND Departures.Reviewer IS NULL

UNION ALL

SELECT
  Arrivals.Id,
  @kind AS Type,
  Arrivals.ReceivedOn,
  Arrivals.Reviewer,
  Arrivals.Status,
  COALESCE(Skippers.Callsign, VoyageMasters.Callsign) AS MasterCallsign
FROM Arrivals
LEFT JOIN Voyages ON Arrivals.VoyageId = Voyages.Id
LEFT JOIN VoyageMasters ON Voyages.Id = VoyageMasters.Id
LEFT JOIN Skippers ON VoyageMasters.SkipperId = Skippers.Id
WHERE Arrivals.Status = @state
  AND Arrivals.Reviewer IS NULL
`,
			want: []string{"Arrivals", "Departures", "Skippers", "VoyageMasters", "Voyages"},
		},
		{
			name:   "one table grouped by an unqualified column",
			tables: []string{"Watches"},
			sql: `SELECT
  Watches.Lookout,
FROM Watches
GROUP BY Lookout
`,
			want: []string{"Watches"},
		},
		{
			name:   "an anti-join: a left join whose WHERE wants the joined row missing, and IS TRUE",
			tables: []string{"Chandlers", "Patrons"},
			sql: `SELECT 
  Chandlers.Id,
  Chandlers.Trade,
  Chandlers.Hail,
  Chandlers.CreatedAt,
FROM Chandlers
LEFT JOIN Patrons ON Chandlers.Id = Patrons.ChandlerId
WHERE Patrons.ChandlerId IS NULL
AND Chandlers.HailOptIn IS TRUE
`,
			want: []string{"Chandlers", "Patrons"},
		},
		{
			name:   "MAX over GREATEST over COALESCEs with typed TIMESTAMP literals, a join and a left join, a line comment ending the WHERE, a one-line GROUP BY",
			tables: []string{"Patrons", "Logins", "Watches"},
			sql: `SELECT
  Patrons.Id,
  Patrons.Hail,
  MAX(GREATEST(
    COALESCE(Patrons.UpdatedAt, TIMESTAMP '2000-01-01T00:00:00Z'),
    COALESCE(Patrons.CreatedAt, TIMESTAMP '2000-01-01T00:00:00Z'),
    COALESCE(Watches.UpdatedAt, TIMESTAMP '2000-01-01T00:00:00Z'),
    COALESCE(Watches.CreatedAt, TIMESTAMP '2000-01-01T00:00:00Z')
  )) AS LastSeenAt,
  Logins.Handle,
  Patrons.HailOptIn
FROM Patrons
JOIN Logins ON Patrons.Id = Logins.Id
LEFT JOIN Watches ON Logins.Handle = Watches.Handle
WHERE Patrons.FinishedAt IS NULL -- the finished stay out
GROUP BY
  Patrons.Id, Patrons.Hail, Logins.Handle, Patrons.HailOptIn
`,
			want: []string{"Logins", "Patrons", "Watches"},
		},
		{
			name:   "two joins and a WHERE on a column named Type against a literal spelled like a table",
			tables: []string{"Operators", "Harbors", "Piers"},
			sql: `SELECT
  Operators.Id as OperatorId,
  Piers.Id as PierId,
  Harbors.Id as HarborId,
  Piers.Code as PierCode
FROM Operators
JOIN Harbors ON Operators.HarborId = Harbors.Id
JOIN Piers ON Harbors.Id = Piers.HarborId
WHERE Operators.Type = 'Harbor'
`,
			want: []string{"Harbors", "Operators", "Piers"},
		},
		{
			name:   "a cross join written as JOIN ON TRUE, a left join on two conditions with two spaces before ON, an anti-join WHERE",
			tables: []string{"Piers", "AmenityKinds", "Amenities"},
			sql: `SELECT
  AmenityKinds.Id AS KindId,
  AmenityKinds.Blurb,
  Piers.Id AS PierId
FROM Piers
JOIN AmenityKinds ON TRUE
LEFT JOIN Amenities  ON Amenities.PierId = Piers.Id AND Amenities.Kind = AmenityKinds.Id
WHERE Amenities.Id IS NULL
`,
			want: []string{"Amenities", "AmenityKinds", "Piers"},
		},
		{
			name:   "a bare CASE expression kept in its own file, one arm an EXISTS subquery with an INTERVAL arithmetic in parentheses",
			tables: []string{"Logins", "Patrons", "Watches"},
			sql: `    CASE
        WHEN Logins.Suspended = TRUE THEN 'moored'
        WHEN Patrons.FinishedAt IS NOT NULL THEN 'finished'
        WHEN Patrons.Id IS NULL THEN 'invited'
        WHEN EXISTS (
            SELECT 1
            FROM Watches
            WHERE Watches.Handle = Logins.Handle
                AND Watches.UpdatedAt > (CURRENT_TIMESTAMP() - INTERVAL 2 WEEK)
        ) THEN 'underway'
        ELSE 'idle'
    END
`,
			want: []string{"Watches"},
		},
		{
			name:   "two joins from a table the schema no longer has: refused at the FROM, the two real tables notwithstanding",
			tables: []string{"Patrons", "Operators"},
			sql: `SELECT
	PatronBerthings.PatronId,
	PatronBerthings.FinishedAt,
	Operators.Id AS OperatorId
FROM PatronBerthings
JOIN Patrons ON Patrons.Id = PatronBerthings.PatronId
JOIN Operators ON Operators.Id = Patrons.OperatorId
`,
			wantErr: "names PatronBerthings at line 5, column 6 in a table position",
		},
		{
			name:   "COALESCE over both sides of a left join, an alias introduced by As, a CASE closed by END AS, an ORDER BY",
			tables: []string{"Invitations", "Members"},
			sql: `SELECT 
  Invitations.Id,
  Invitations.OperatorId,
  COALESCE(Members.GivenName, Invitations.GivenName) AS GivenName,
  COALESCE(Members.Surname, Invitations.Surname) AS Surname,
  Invitations.Hail,
  Members.Id As MemberId,
  CASE
    WHEN Members.Id IS NULL THEN 'invited'
    ELSE 'active'
    END AS Standing
FROM Invitations
LEFT JOIN Members ON Invitations.Id = Members.InvitationId
ORDER BY Invitations.Id
`,
			want: []string{"Invitations", "Members"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := queryTables(tt.sql, tableNames(tt.tables...))
			if (err != nil) != (tt.wantErr != "") {
				t.Fatalf("queryTables() error = %v, wantErr %v", err, tt.wantErr != "")
			}
			if err != nil {
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("queryTables() error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("queryTables() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// tableNames is a schema's table map as the walk takes it: each name by its lowercase.
func tableNames(names ...string) map[string]string {
	tables := make(map[string]string, len(names))
	for _, name := range names {
		tables[strings.ToLower(name)] = name
	}

	return tables
}
