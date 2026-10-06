// Package main loads synthetic volume over the seeded Lodestar world for reading real
// query plans: sectors with wings, squadrons, memberships, hangars, and ships; missions
// spread over every sector, Anvil included, and assigned across each sector's squadrons;
// sorties and droid reports on top. Rows are written as batched insert mutations, never
// DDL, in foreign-key order, from deterministic identifiers, so a second run collides on
// its keys rather than doubling the world, and a bootstrap -reset removes it all. It
// reaches the database the way the bootstrap does: the environment names the target.
//
// It only makes sense against a real instance, where the optimizer's plans are real; on
// the emulator it merely fills tables.
//
// Demonstrates: bootstrap.volume.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"sync"
	"time"

	"cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/resource/lodestar/pkg/config"
	"github.com/go-playground/errors/v5"
)

// scale is one preset of the synthetic world's size.
type scale struct {
	sectors           int // synthetic sectors, added beside anvil, bastion, and cinder
	wingsPerSector    int
	squadronsPerWing  int
	usersPerSquadron  int
	hangarsPerSector  int
	shipsPerHangar    int
	missionsPerSector int // per sector, Anvil included
	clients           int
}

var scales = map[string]scale{
	"medium": {sectors: 20, wingsPerSector: 2, squadronsPerWing: 3, usersPerSquadron: 20, hangarsPerSector: 4, shipsPerHangar: 50, missionsPerSector: 2500, clients: 20},
	"large":  {sectors: 50, wingsPerSector: 2, squadronsPerWing: 3, usersPerSquadron: 50, hangarsPerSector: 4, shipsPerHangar: 100, missionsPerSector: 10000, clients: 50},
}

// The seeded rows the synthetic world attaches to.
const (
	anvil          = "anvil"
	forgeWingID    = "40000000-0000-4000-8000-000000000001"
	rowsPerCommit  = 1000
	sortieEvery    = 5 // one sortie per this many missions
	reportsPerShip = 2
	certifiedEvery = 2 // every other mission requires a certification
	daySpread      = 360
)

// Column names the rows share, and the statuses the mix names more than once.
const (
	colID           = "Id"
	colSectorID     = "SectorId"
	colName         = "Name"
	statusOpen      = "open"
	statusClaimed   = "claimed"
	statusUnderway  = "underway"
	statusCompleted = "completed"
)

var (
	kinds   = []string{"courier", "escort", "rescue", "salvage"}
	certs   = []string{"deep_space", "escort", "hazmat", "salvage"}
	classes = []string{"20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002", "20000000-0000-4000-8000-000000000003", "20000000-0000-4000-8000-000000000004"}
	// statuses is the mix one sector's missions cycle through: mostly open and underway,
	// a fifth completed, a few held, stood down, or failed.
	statuses = []string{
		statusOpen, statusOpen, statusOpen, statusOpen, statusClaimed, statusClaimed, statusUnderway, statusUnderway, statusUnderway,
		"on_hold", statusCompleted, statusCompleted, statusCompleted, "stood_down", "failed",
	}
)

func main() {
	name := flag.String("scale", "medium", "preset: medium or large")
	parallel := flag.Int("parallel", 8, "commits in flight")
	flag.Parse()

	s, ok := scales[*name]
	if !ok {
		log.Fatalf("unknown scale %q; use medium or large", *name)
	}
	if err := run(context.Background(), s, *parallel); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, s scale, parallel int) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	settings, err := config.LoadSpannerSettings(ctx)
	if err != nil {
		return errors.Wrap(err, "config.LoadSpannerSettings()")
	}
	client, err := spanner.NewClient(ctx, settings.DatabasePath())
	if err != nil {
		return errors.Wrap(err, "spanner.NewClient()")
	}
	defer client.Close()

	w := &world{s: s, loader: &loader{client: client, slots: make(chan struct{}, parallel)}}
	fmt.Printf("Loading the %+v world into %s\n", s, settings.DatabasePath())

	// Foreign-key order, one table at a time: a child's commit finds its parents.
	for _, step := range []struct {
		table string
		load  func(context.Context) error
	}{
		{"Sectors", w.sectors},
		{"Wings", w.wings},
		{"Squadrons", w.squadrons},
		{"SquadronMemberships", w.memberships},
		{"Clients", w.clients},
		{"Hangars", w.hangars},
		{"Ships", w.ships},
		{"Missions", w.missions},
		{"Sorties", w.sorties},
		{"DroidReports", w.droidReports},
	} {
		started := time.Now()
		if err := step.load(ctx); err != nil {
			return errors.Wrapf(err, "loading %s", step.table)
		}
		if err := w.loader.flush(ctx); err != nil {
			return errors.Wrapf(err, "loading %s", step.table)
		}
		fmt.Printf("%-20s %8d rows in %6.1fs\n", step.table, w.loader.take(), time.Since(started).Seconds())
	}

	return nil
}

// loader batches insert mutations into commits of rowsPerCommit rows and applies up to
// cap(slots) commits concurrently, keeping the first error.
type loader struct {
	client  *spanner.Client
	slots   chan struct{}
	pending []*spanner.Mutation
	wg      sync.WaitGroup
	mu      sync.Mutex
	err     error
	rows    int
}

func (l *loader) add(ctx context.Context, m *spanner.Mutation) error {
	l.pending = append(l.pending, m)
	if len(l.pending) < rowsPerCommit {
		return nil
	}

	return l.commit(ctx)
}

// commit hands the pending rows to a worker; it blocks only while every slot is busy.
func (l *loader) commit(ctx context.Context) error {
	if len(l.pending) == 0 {
		return nil
	}
	if err := l.firstError(); err != nil {
		return err
	}
	batch := l.pending
	l.pending = nil

	select {
	case l.slots <- struct{}{}:
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "waiting for a commit slot")
	}
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		defer func() {
			<-l.slots
		}()
		if _, err := l.client.Apply(ctx, batch); err != nil {
			l.record(errors.Wrap(err, "spanner.Client.Apply()"))

			return
		}
		l.mu.Lock()
		l.rows += len(batch)
		l.mu.Unlock()
	}()

	return nil
}

// flush commits what is pending and waits for every commit in flight.
func (l *loader) flush(ctx context.Context) error {
	if err := l.commit(ctx); err != nil {
		return err
	}
	l.wg.Wait()

	return l.firstError()
}

func (l *loader) record(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err == nil {
		l.err = err
	}
}

func (l *loader) firstError() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.err
}

// take returns and resets the count of rows committed since the last call.
func (l *loader) take() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := l.rows
	l.rows = 0

	return n
}

// world derives every synthetic row from its scale and deterministic counters.
type world struct {
	s      scale
	loader *loader
}

// sectorIDs lists every sector the world spans: Anvil first, then the synthetic ones.
func (w *world) sectorIDs() []string {
	ids := make([]string, 0, w.s.sectors+1)
	ids = append(ids, anvil)
	for i := range w.s.sectors {
		ids = append(ids, fmt.Sprintf("vol-%03d", i))
	}

	return ids
}

// Identifier schemes: valid UUIDs under a hex prefix the seed never uses.
func wingID(sector, i int) string {
	return fmt.Sprintf("e1000000-0000-4000-8000-%06d%06d", sector, i)
}

func squadronID(sector, wing, i int) string {
	return fmt.Sprintf("e2000000-0000-4000-8000-%04d%04d%04d", sector, wing, i)
}

func clientID(i int) string {
	return fmt.Sprintf("e3000000-0000-4000-8000-%012d", i)
}

func hangarID(sector, i int) string {
	return fmt.Sprintf("e4000000-0000-4000-8000-%06d%06d", sector, i)
}

func shipID(sector, hangar, i int) string {
	return fmt.Sprintf("e5000000-0000-4000-8000-%04d%04d%04d", sector, hangar, i)
}

func missionID(sector, i int) string {
	return fmt.Sprintf("e6000000-0000-4000-8000-%04d%08d", sector, i)
}

func sortieID(sector, i int) string {
	return fmt.Sprintf("e7000000-0000-4000-8000-%04d%08d", sector, i)
}

func reportID(sector, i int) string {
	return fmt.Sprintf("e8000000-0000-4000-8000-%04d%08d", sector, i)
}

func userID(sector, i int) string {
	return fmt.Sprintf("vol-user-%03d-%05d", sector, i)
}

// wingIDs lists a sector's wings: Anvil keeps Forge Wing and gains the rest.
func (w *world) wingIDs(sector int) []string {
	ids := make([]string, 0, w.s.wingsPerSector)
	if sector == 0 {
		ids = append(ids, forgeWingID)
	}
	for i := len(ids); i < w.s.wingsPerSector; i++ {
		ids = append(ids, wingID(sector, i))
	}

	return ids
}

// squadronIDs lists a sector's squadrons across its wings.
func (w *world) squadronIDs(sector int) []string {
	ids := make([]string, 0, w.s.wingsPerSector*w.s.squadronsPerWing)
	for wing := range w.s.wingsPerSector {
		for i := range w.s.squadronsPerWing {
			ids = append(ids, squadronID(sector, wing, i))
		}
	}

	return ids
}

func (w *world) shipIDs(sector int) []string {
	ids := make([]string, 0, w.s.hangarsPerSector*w.s.shipsPerHangar)
	for hangar := range w.s.hangarsPerSector {
		for i := range w.s.shipsPerHangar {
			ids = append(ids, shipID(sector, hangar, i))
		}
	}

	return ids
}

func (w *world) sectors(ctx context.Context) error {
	for i, id := range w.sectorIDs()[1:] {
		m := spanner.InsertMap("Sectors", map[string]any{
			colID: id, colName: fmt.Sprintf("Volume sector %03d", i), "Region": "Synthetic reach",
			"Established": civil.Date{Year: 2200 + i%50, Month: time.January, Day: 1},
		})
		if err := w.loader.add(ctx, m); err != nil {
			return err
		}
	}

	return nil
}

func (w *world) wings(ctx context.Context) error {
	for sector, sectorID := range w.sectorIDs() {
		for i, id := range w.wingIDs(sector) {
			if id == forgeWingID {
				continue
			}
			m := spanner.InsertMap("Wings", map[string]any{colID: id, colSectorID: sectorID, colName: fmt.Sprintf("Wing %d of %s", i, sectorID)})
			if err := w.loader.add(ctx, m); err != nil {
				return err
			}
		}
	}

	return nil
}

func (w *world) squadrons(ctx context.Context) error {
	for sector := range w.sectorIDs() {
		wings := w.wingIDs(sector)
		for wing := range w.s.wingsPerSector {
			for i := range w.s.squadronsPerWing {
				m := spanner.InsertMap("Squadrons", map[string]any{colID: squadronID(sector, wing, i), "WingId": wings[wing], colName: fmt.Sprintf("Squadron %d-%d", wing, i)})
				if err := w.loader.add(ctx, m); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// memberships put each synthetic user in one squadron, and every fifth in a second.
func (w *world) memberships(ctx context.Context) error {
	for sector := range w.sectorIDs() {
		squadrons := w.squadronIDs(sector)
		for q, squadron := range squadrons {
			for i := range w.s.usersPerSquadron {
				user := userID(sector, q*w.s.usersPerSquadron+i)
				if err := w.loader.add(ctx, spanner.InsertMap("SquadronMemberships", map[string]any{"SquadronId": squadron, "UserId": user})); err != nil {
					return err
				}
				if i%5 == 0 {
					other := squadrons[(q+1)%len(squadrons)]
					if err := w.loader.add(ctx, spanner.InsertMap("SquadronMemberships", map[string]any{"SquadronId": other, "UserId": user})); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func (w *world) clients(ctx context.Context) error {
	for i := range w.s.clients {
		m := spanner.InsertMap("Clients", map[string]any{
			colID: clientID(i), colName: fmt.Sprintf("Volume client %03d", i), "ContactName": "Synthetic contact",
			"ContactEmail": fmt.Sprintf("contact%03d@volume.example", i), "Trusted": i%3 != 0,
		})
		if err := w.loader.add(ctx, m); err != nil {
			return err
		}
	}

	return nil
}

func (w *world) hangars(ctx context.Context) error {
	for sector, sectorID := range w.sectorIDs() {
		for i := range w.s.hangarsPerSector {
			m := spanner.InsertMap("Hangars", map[string]any{colID: hangarID(sector, i), colSectorID: sectorID, colName: fmt.Sprintf("Hangar %d", i), "Zone": fmt.Sprintf("zone-%d", i%3)})
			if err := w.loader.add(ctx, m); err != nil {
				return err
			}
		}
	}

	return nil
}

func (w *world) ships(ctx context.Context) error {
	n := 0
	for sector := range w.sectorIDs() {
		for hangar := range w.s.hangarsPerSector {
			for i := range w.s.shipsPerHangar {
				n++
				m := spanner.InsertMap("Ships", map[string]any{
					colID: shipID(sector, hangar, i), "HangarId": hangarID(sector, hangar), "ClassId": classes[n%len(classes)],
					"Registry": fmt.Sprintf("VOL-%07d", n), colName: fmt.Sprintf("Volume ship %07d", n), "UpdatedAt": spanner.CommitTimestamp,
				})
				if err := w.loader.add(ctx, m); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// missions spread each sector's missions over its squadrons and the clients, with a
// status mix, a hazard cycle, deadlines a year wide around now, and a certification on
// every other one.
func (w *world) missions(ctx context.Context) error {
	now := time.Now().UTC()
	for sector, sectorID := range w.sectorIDs() {
		squadrons := w.squadronIDs(sector)
		if sector == 0 {
			// Anvil's own squadrons take their share of the volume too.
			squadrons = append(squadrons, "50000000-0000-4000-8000-000000000001", "50000000-0000-4000-8000-000000000002")
		}
		for i := range w.s.missionsPerSector {
			status := statuses[i%len(statuses)]
			row := map[string]any{
				colID: missionID(sector, i), colSectorID: sectorID, "ClientId": clientID(i % w.s.clients), "KindId": kinds[i%len(kinds)],
				"Title": fmt.Sprintf("Volume mission %d in %s", i, sectorID), "Hazard": int64(1 + i%5),
				"Fee": big.NewRat(int64(1000+(i*7919)%99000), 1), "Deadline": now.Add(time.Duration(i%daySpread-daySpread/2) * 24 * time.Hour),
				"BookedBy": "booking", "StatusId": status,
			}
			if i%certifiedEvery == 0 {
				row["RequiredCertId"] = certs[i%len(certs)]
			}
			if status != statusOpen {
				row["AssignedSquadronId"] = squadrons[i%len(squadrons)]
			}
			if status == statusCompleted {
				row["Settlement"] = big.NewRat(int64(500+(i*104729)%90000), 1)
			}
			if err := w.loader.add(ctx, spanner.InsertMap("Missions", row)); err != nil {
				return err
			}
		}
	}

	return nil
}

func (w *world) sorties(ctx context.Context) error {
	now := time.Now().UTC()
	for sector := range w.sectorIDs() {
		ships := w.shipIDs(sector)
		for i := 0; i < w.s.missionsPerSector; i += sortieEvery {
			m := spanner.InsertMap("Sorties", map[string]any{
				colID: sortieID(sector, i), "MissionId": missionID(sector, i), "ShipId": ships[i%len(ships)],
				"PilotUserId": userID(sector, i%(w.s.usersPerSquadron*w.s.wingsPerSector*w.s.squadronsPerWing)),
				"LaunchedAt":  now.Add(-time.Duration(i%daySpread) * 24 * time.Hour),
			})
			if err := w.loader.add(ctx, m); err != nil {
				return err
			}
		}
	}

	return nil
}

func (w *world) droidReports(ctx context.Context) error {
	now := time.Now().UTC()
	for sector, sectorID := range w.sectorIDs() {
		for i, ship := range w.shipIDs(sector) {
			for r := range reportsPerShip {
				m := spanner.InsertMap("DroidReports", map[string]any{
					colID: reportID(sector, i*reportsPerShip+r), colSectorID: sectorID, "ShipId": ship, "Subsystem": []string{"drive", "hull"}[r%2],
					"Reading": float64((i*31+r*17)%100) / 100, "RecordedAt": now.Add(-time.Duration(i%daySpread) * time.Hour),
				})
				if err := w.loader.add(ctx, m); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
