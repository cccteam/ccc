package resource

// The semantic differential (design plan §11, backlog #52): the rendered SQL
// must mean what the condition text says. A seeded property draws random
// conditional grants and random rows, writes the rows to the Spanner
// emulator, runs the read statement, the write check-SELECT, and the
// capability booleans the package renders, and compares each answer with the
// reference evaluator in conditiontest, which implements the language's
// stated meaning over a row image and knows no schema. The surviving row set,
// each conditional cell and its masked name, the order and the filter over a
// conditional column, every check group boolean, and every capability answer
// must agree.
//
// Decisions are stubbed the way the engine folds: a grant whose condition
// folds TRUE against the request's facts is Granted, FALSE is Denied,
// anything else is Conditional carrying the residue. One database serves the
// run and each case owns one partition, so nothing is ever deleted. The
// generators are seeded; a failure prints the seed, the case, the policy, the
// statement and its parameters, and the row with expected and actual answers.

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/accesstypes/condition"
	"github.com/cccteam/ccc/accesstypes/condition/conditiontest"
	"github.com/shopspring/decimal"
)

const (
	semanticSeed               = 20260914
	semanticCases       uint64 = 100
	semanticRowsPerCase        = 64
	semanticWriteSample        = 6
)

// semanticPools are the stored-value pools, overlapping the generator's
// literal pools so equal, less, greater, and no value all occur, with NUMERIC
// and INT64 neighbors of the past-double-precision literal so a comparison
// routed through FLOAT64 is caught.
type semanticPools struct {
	strings  []string
	ints     []int64
	floats   []float64
	numerics []decimal.Decimal
	times    []time.Time
	dates    []civil.Date
	zones    []*time.Location
}

func newSemanticPools(t *testing.T) *semanticPools {
	t.Helper()

	p := &semanticPools{}
	for _, lit := range conditiontest.LiteralPool(AttributeTypeString) {
		if s, ok := lit.(condition.StringLiteral); ok {
			p.strings = append(p.strings, s.Value)
		}
	}
	p.strings = append(p.strings, "zeta", "Open", "closed")

	for _, lit := range conditiontest.LiteralPool(AttributeTypeNumber) {
		n, ok := lit.(condition.NumberLiteral)
		if !ok {
			continue
		}
		d := decimal.RequireFromString(n.Text)
		p.numerics = append(p.numerics, d)
		f, _ := d.Float64()
		p.floats = append(p.floats, f)
		if d.IsInteger() {
			p.ints = append(p.ints, d.IntPart())
		}
	}
	p.ints = append(p.ints, 7, 11, 43, 1234567890123456789, 1234567890123456788)
	p.floats = append(p.floats, 0.1, 42.5)
	for _, text := range []string{"10.500000001", "1234567890123456788.5", "1234567890.123456789", "-0.250000001"} {
		p.numerics = append(p.numerics, decimal.RequireFromString(text))
	}

	for _, lit := range conditiontest.LiteralPool(AttributeTypeTimestamp) {
		if s, ok := lit.(condition.StringLiteral); ok {
			instant, err := time.Parse(time.RFC3339, s.Value)
			if err != nil {
				t.Fatal(err)
			}
			p.times = append(p.times, instant.UTC())
		}
	}
	p.times = append(p.times,
		time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1999, 12, 31, 23, 59, 58, 0, time.UTC),
		time.Date(2026, 1, 2, 15, 4, 4, 0, time.UTC),
	)

	for _, lit := range conditiontest.LiteralPool(AttributeTypeDate) {
		if s, ok := lit.(condition.StringLiteral); ok {
			d, err := civil.ParseDate(s.Value)
			if err != nil {
				t.Fatal(err)
			}
			p.dates = append(p.dates, d)
		}
	}
	p.dates = append(p.dates, civil.Date{Year: 2026, Month: 1, Day: 3}, civil.Date{Year: 2000, Month: 1, Day: 1})

	for _, name := range []string{"UTC", "America/Denver", "Asia/Tokyo"} {
		zone, err := condition.LoadZone(name)
		if err != nil {
			t.Fatal(err)
		}
		p.zones = append(p.zones, zone)
	}

	return p
}

func draw[T any](rng *rand.Rand, pool []T) T {
	return pool[rng.IntN(len(pool))]
}

// maybe draws a pool value or nothing, one in four.
func maybe[T any](rng *rand.Rand, pool []T) *T {
	if rng.IntN(4) == 0 {
		return nil
	}
	v := draw(rng, pool)

	return &v
}

// semanticWorld is one case's data: the partition's rows, the related rows
// they reach, the requester's anchor rows (and other users' as noise), and
// the request's facts.
type semanticWorld struct {
	index       uint64
	depot       string
	requester   string
	now         time.Time
	zone        *time.Location
	parcels     []*semanticParcel
	carriers    map[string]*semanticCarrier
	routes      map[string]*semanticRoute
	hubs        map[string]*semanticHub
	memberships []*semanticMembership
	profiles    []*semanticProfile
}

func (w *semanticWorld) scope() accesstypes.Scope {
	return accesstypes.DomainScope(accesstypes.Domain(w.depot))
}

func (w *semanticWorld) facts() condition.Facts {
	return condition.NewFacts().WithSubject(w.requester).WithNow(w.now).WithZone(w.zone)
}

func (w *semanticWorld) env() accesstypes.Environment {
	return accesstypes.EnvironmentAt(w.now).WithZone(w.zone)
}

// genWorld draws one case's rows.
func genWorld(rng *rand.Rand, pools *semanticPools, index uint64) *semanticWorld {
	w := &semanticWorld{
		index:     index,
		depot:     fmt.Sprintf("depot-%03d", index),
		requester: fmt.Sprintf("user-%03d", index),
		now:       draw(rng, pools.times),
		zone:      draw(rng, pools.zones),
		carriers:  make(map[string]*semanticCarrier),
		routes:    make(map[string]*semanticRoute),
		hubs:      make(map[string]*semanticHub),
	}

	hubIDs := make([]string, 0, 3)
	carrierIDs := make([]string, 0, 3)
	routeIDs := make([]string, 0, 3)
	for i := range 3 {
		hub := &semanticHub{ID: fmt.Sprintf("hub-%03d-%d", index, i), Region: maybe(rng, pools.strings)}
		w.hubs[hub.ID] = hub
		hubIDs = append(hubIDs, hub.ID)
	}
	for i := range 3 {
		carrier := &semanticCarrier{ID: fmt.Sprintf("carrier-%03d-%d", index, i), Code: maybe(rng, pools.strings)}
		w.carriers[carrier.ID] = carrier
		carrierIDs = append(carrierIDs, carrier.ID)
		route := &semanticRoute{ID: fmt.Sprintf("route-%03d-%d", index, i), HubID: maybe(rng, hubIDs)}
		w.routes[route.ID] = route
		routeIDs = append(routeIDs, route.ID)
	}

	for range semanticRowsPerCase {
		w.parcels = append(w.parcels, genParcel(rng, pools, w, carrierIDs, routeIDs))
	}

	// The requester's memberships in the partition, one in another partition
	// that the derived tenancy filter must exclude, and other users' as noise.
	for i := range rng.IntN(4) {
		w.memberships = append(w.memberships, genMembership(rng, pools, fmt.Sprintf("m-%03d-%d", index, i), w.requester, w.depot, hubIDs))
	}
	w.memberships = append(w.memberships, genMembership(rng, pools, fmt.Sprintf("m-%03d-other", index), w.requester, w.depot+"-other", hubIDs))
	for i := range 2 {
		w.memberships = append(w.memberships, genMembership(rng, pools, fmt.Sprintf("m-%03d-noise-%d", index, i), fmt.Sprintf("noise-%03d-%d", index, i), w.depot, hubIDs))
	}

	// The requester's profile, half the time, and one other user's always.
	if rng.IntN(2) == 0 {
		w.profiles = append(w.profiles, genProfile(rng, pools, w.requester, hubIDs))
	}
	w.profiles = append(w.profiles, genProfile(rng, pools, fmt.Sprintf("noise-%03d-0", index), hubIDs))

	return w
}

// genParcel draws one row of the checked resource in the world's partition.
func genParcel(rng *rand.Rand, pools *semanticPools, w *semanticWorld, carrierIDs, routeIDs []string) *semanticParcel {
	p := &semanticParcel{
		ID:          ccc.Must(ccc.NewUUID()),
		Depot:       w.depot,
		Label:       draw(rng, pools.strings),
		Note:        maybe(rng, pools.strings),
		Weight:      draw(rng, pools.ints),
		Pieces:      maybe(rng, pools.ints),
		Ratio:       draw(rng, pools.floats),
		Density:     maybe(rng, pools.floats),
		Price:       draw(rng, pools.numerics),
		Fragile:     rng.IntN(2) == 0,
		Insured:     maybe(rng, []bool{true, false}),
		ShippedAt:   draw(rng, pools.times),
		DeliveredAt: maybe(rng, pools.times),
		ShipDate:    draw(rng, pools.dates),
		DueDate:     maybe(rng, pools.dates),
		CarrierID:   maybe(rng, carrierIDs),
		RouteID:     maybe(rng, routeIDs),
	}
	if fee := maybe(rng, pools.numerics); fee != nil {
		p.Fee = decimal.NewNullDecimal(*fee)
	}

	return p
}

func genMembership(rng *rand.Rand, pools *semanticPools, id, user, depot string, hubIDs []string) *semanticMembership {
	return &semanticMembership{
		ID:     id,
		UserID: user,
		Depot:  depot,
		Team:   maybe(rng, pools.strings),
		Tier:   maybe(rng, pools.ints),
		HubID:  maybe(rng, hubIDs),
	}
}

func genProfile(rng *rand.Rand, pools *semanticPools, user string, hubIDs []string) *semanticProfile {
	p := &semanticProfile{
		UserID:       user,
		Nickname:     maybe(rng, pools.strings),
		Quota:        maybe(rng, pools.ints),
		Rate:         maybe(rng, pools.floats),
		Active:       maybe(rng, []bool{true, false}),
		ClearedUntil: maybe(rng, pools.times),
		StartDate:    maybe(rng, pools.dates),
		HomeHubID:    maybe(rng, hubIDs),
	}
	if allowance := maybe(rng, pools.numerics); allowance != nil {
		p.Allowance = decimal.NewNullDecimal(*allowance)
	}

	return p
}

// mutations renders the world as insert mutations. Rows go through
// insertRow rather than spanner.InsertStruct, which cannot encode the decimal
// types the resource struct carries.
func (w *semanticWorld) mutations() []*spanner.Mutation {
	var muts []*spanner.Mutation
	add := func(table string, row any) {
		muts = append(muts, insertRow(table, row))
	}
	for _, hub := range w.hubs {
		add("Hubs", hub)
	}
	for _, carrier := range w.carriers {
		add("Carriers", carrier)
	}
	for _, route := range w.routes {
		add("Routes", route)
	}
	for _, parcel := range w.parcels {
		add(string(semanticResource), parcel)
	}
	for _, membership := range w.memberships {
		add("Memberships", membership)
	}
	for _, profile := range w.profiles {
		add("Profiles", profile)
	}

	return muts
}

// deletions removes the world's rows, referencing tables first.
func (w *semanticWorld) deletions() []*spanner.Mutation {
	var muts []*spanner.Mutation
	del := func(table string, keys []spanner.Key) {
		muts = append(muts, spanner.Delete(table, spanner.KeySetFromKeys(keys...)))
	}
	parcels := make([]spanner.Key, 0, len(w.parcels))
	for _, p := range w.parcels {
		parcels = append(parcels, spanner.Key{p.ID.String()})
	}
	del(string(semanticResource), parcels)
	memberships := make([]spanner.Key, 0, len(w.memberships))
	for _, m := range w.memberships {
		memberships = append(memberships, spanner.Key{m.ID})
	}
	del("Memberships", memberships)
	profiles := make([]spanner.Key, 0, len(w.profiles))
	for _, p := range w.profiles {
		profiles = append(profiles, spanner.Key{p.UserID})
	}
	del("Profiles", profiles)
	del("Routes", stringKeys(maps.Keys(w.routes)))
	del("Carriers", stringKeys(maps.Keys(w.carriers)))
	del("Hubs", stringKeys(maps.Keys(w.hubs)))

	return muts
}

func stringKeys(ids iter.Seq[string]) []spanner.Key {
	var keys []spanner.Key
	for id := range ids {
		keys = append(keys, spanner.Key{id})
	}

	return keys
}

// insertRow builds an insert mutation from a spanner-tagged struct, each value
// normalized the way the package binds parameters (NUMERIC as big.Rat).
func insertRow(table string, row any) *spanner.Mutation {
	v := reflect.ValueOf(row).Elem()
	columns := make(map[string]any, v.NumField())
	for i := range v.NumField() {
		column := v.Type().Field(i).Tag.Get("spanner")
		columns[column] = paramValue(v.Field(i).Interface())
	}

	return spanner.InsertMap(table, columns)
}

// carrierCode resolves the one-hop attribute: the carrier's code, or nothing.
func (w *semanticWorld) carrierCode(carrierID *string) any {
	if carrierID == nil {
		return nil
	}
	carrier, ok := w.carriers[*carrierID]
	if !ok {
		return nil
	}

	return semanticValue(carrier.Code)
}

// hubRegion resolves the two-hop attribute: the route's hub's region, or
// nothing at any missing step.
func (w *semanticWorld) hubRegion(routeID *string) any {
	if routeID == nil {
		return nil
	}
	route, ok := w.routes[*routeID]
	if !ok || route.HubID == nil {
		return nil
	}
	hub, ok := w.hubs[*route.HubID]
	if !ok {
		return nil
	}

	return semanticValue(hub.Region)
}

// image assembles the evaluator's view of one row: the pre-image attribute
// values (join paths resolved), the post-image overlay for a mutation, the
// requester's subject sets restricted to the partition, the requester's
// subject values, and the facts.
func (w *semanticWorld) image(p *semanticParcel, post map[string]any) *conditiontest.Image {
	pre := make(map[string]any, len(semanticColumnAttributes)+2)
	for _, attr := range semanticColumnAttributes {
		pre[attr.name] = semanticValue(semanticFieldValue(p, attr.field))
	}
	pre["carrierCode"] = w.carrierCode(p.CarrierID)
	pre["hubRegion"] = w.hubRegion(p.RouteID)

	sets := map[string][]any{}
	for _, m := range w.memberships {
		if m.UserID != w.requester || m.Depot != w.depot {
			continue
		}
		if v := semanticValue(m.Team); v != nil {
			sets["teams"] = append(sets["teams"], v)
		}
		if v := semanticValue(m.Tier); v != nil {
			sets["tiers"] = append(sets["tiers"], v)
		}
		if m.HubID != nil {
			if hub, ok := w.hubs[*m.HubID]; ok {
				if v := semanticValue(hub.Region); v != nil {
					sets["regions"] = append(sets["regions"], v)
				}
			}
		}
	}

	values := map[string]any{}
	for _, profile := range w.profiles {
		if profile.UserID != w.requester {
			continue
		}
		values["nickname"] = semanticValue(profile.Nickname)
		values["quota"] = semanticValue(profile.Quota)
		values["rate"] = semanticValue(profile.Rate)
		values["allowance"] = semanticValue(profile.Allowance)
		values["active"] = semanticValue(profile.Active)
		values["clearedUntil"] = semanticValue(profile.ClearedUntil)
		values["startDate"] = semanticValue(profile.StartDate)
		values["homeRegion"] = w.hubRegion(nil)
		if profile.HomeHubID != nil {
			if hub, ok := w.hubs[*profile.HomeHubID]; ok {
				values["homeRegion"] = semanticValue(hub.Region)
			}
		}
	}

	return &conditiontest.Image{
		Pre:           pre,
		Post:          post,
		Subject:       w.requester,
		Now:           w.now,
		Zone:          w.zone,
		SubjectSets:   sets,
		SubjectValues: values,
	}
}

// semanticGrant is one drawn grant: a permission over a field set (nil for
// the base resource, the delete's footprint) under a condition (nil for an
// unconditional grant).
type semanticGrant struct {
	perm   accesstypes.Permission
	fields []accesstypes.Field
	expr   condition.Expr
}

type semanticPolicy struct {
	grants []semanticGrant
}

func (p *semanticPolicy) String() string {
	var b strings.Builder
	for _, g := range p.grants {
		fields := "<resource>"
		if g.fields != nil {
			fields = fmt.Sprint(g.fields)
		}
		cond := "<unconditional>"
		if g.expr != nil {
			cond = g.expr.String()
		}
		fmt.Fprintf(&b, "  %s %s: %s\n", g.perm, fields, cond)
	}

	return b.String()
}

// stringAttributes are the attributes the implication-covered pair may use.
var semanticStringAttributes = []string{"label", "note", "carrierCode", "hubRegion"}

// genFieldGrants draws one permission's grants over the fields: always the
// implication-covered pair from #61 (a = v beside a IN (v, w) on one
// attribute), up to two generated conditions, and — where asked — one
// unconditional grant on a single field so the row predicate is TRUE and
// nothing prunes. With coverAll every field ends up under some grant.
func genFieldGrants(rng *rand.Rand, pools *semanticPools, perm accesstypes.Permission, gen *conditiontest.Generator, coverAll, unconditional bool) []semanticGrant {
	fields := make([]accesstypes.Field, 0, len(semanticFields))
	for _, f := range semanticFields {
		fields = append(fields, f.field)
	}
	subset := func() []accesstypes.Field {
		rng.Shuffle(len(fields), func(i, j int) {
			fields[i], fields[j] = fields[j], fields[i]
		})
		n := 1 + rng.IntN(len(fields))

		return slices.Clone(fields[:n])
	}

	attr := condition.Ref{Name: draw(rng, semanticStringAttributes)}
	v := draw(rng, pools.strings)
	w := draw(rng, pools.strings)
	for w == v {
		w = draw(rng, pools.strings)
	}
	grants := []semanticGrant{
		{perm: perm, fields: subset(), expr: condition.Comparison{Left: attr, Op: condition.Eq, Right: condition.StringLiteral{Value: v}}},
		{perm: perm, fields: subset(), expr: condition.In{Left: attr, Literals: []condition.Literal{condition.StringLiteral{Value: v}, condition.StringLiteral{Value: w}}}},
	}
	for range rng.IntN(3) {
		grants = append(grants, semanticGrant{perm: perm, fields: subset(), expr: gen.Expr(2 + rng.IntN(2))})
	}
	if unconditional {
		grants = append(grants, semanticGrant{perm: perm, fields: []accesstypes.Field{draw(rng, fields)}})
	}

	if coverAll {
		for _, field := range fields {
			covered := slices.ContainsFunc(grants, func(g semanticGrant) bool {
				return slices.Contains(g.fields, field)
			})
			if !covered {
				i := rng.IntN(len(grants))
				grants[i].fields = append(grants[i].fields, field)
			}
		}
	}

	return grants
}

// genPolicy draws a case's grants: read grants covering every projected
// field, update and create grants covering a random share, and zero to two
// delete grants on the resource.
func genPolicy(rng *rand.Rand, pools *semanticPools, readGen, writeGen *conditiontest.Generator) *semanticPolicy {
	policy := &semanticPolicy{}
	policy.grants = append(policy.grants, genFieldGrants(rng, pools, accesstypes.List, readGen, true, rng.IntN(4) == 0)...)
	policy.grants = append(policy.grants, genFieldGrants(rng, pools, accesstypes.Update, writeGen, rng.IntN(2) == 0, rng.IntN(3) == 0)...)
	policy.grants = append(policy.grants, genFieldGrants(rng, pools, accesstypes.Create, readGen, rng.IntN(2) == 0, rng.IntN(3) == 0)...)
	for range rng.IntN(3) {
		grant := semanticGrant{perm: accesstypes.Delete}
		if rng.IntN(4) != 0 {
			grant.expr = readGen.Expr(2)
		}
		policy.grants = append(policy.grants, grant)
	}

	return policy
}

// decide mirrors the engine's fold for one checked resource: an unconditional
// covering grant, or one whose condition folds TRUE, is Granted; a condition
// that folds FALSE drops out; the residues combine any-of into one
// Conditional group; no grant left is Denied.
func decide(t *testing.T, res accesstypes.Resource, grants []semanticGrant, facts condition.Facts) accesstypes.Decision {
	t.Helper()

	if len(grants) == 0 {
		return accesstypes.Denied()
	}
	var residues []condition.Expr
	for _, g := range grants {
		if g.expr == nil {
			return accesstypes.Granted()
		}
		folded, err := condition.Fold(g.expr, facts)
		if err != nil {
			t.Fatalf("Fold(%s) error = %v", g.expr.String(), err)
		}
		if truth, ok := folded.(condition.Truth); ok {
			if truth.Value {
				return accesstypes.Granted()
			}

			continue
		}
		residues = append(residues, folded)
	}
	if len(residues) == 0 {
		return accesstypes.Denied()
	}

	return accesstypes.Conditional(accesstypes.ConditionGroup{Resources: []accesstypes.Resource{res}, Condition: accesstypes.ConditionFromExpr(orOf(residues))})
}

// decisions builds one permission's Decisions: the base resource from every
// grant of the permission, each field from the grants covering it.
func (p *semanticPolicy) decisions(t *testing.T, perm accesstypes.Permission, set *Set[semanticParcel], facts condition.Facts) accesstypes.Decisions {
	t.Helper()

	var all []semanticGrant
	for _, g := range p.grants {
		if g.perm == perm {
			all = append(all, g)
		}
	}
	out := accesstypes.Decisions{semanticResource: decide(t, semanticResource, all, facts)}
	for _, f := range semanticFields {
		var covering []semanticGrant
		for _, g := range all {
			if slices.Contains(g.fields, f.field) {
				covering = append(covering, g)
			}
		}
		res := set.Resource(f.field)
		out[res] = decide(t, res, covering, facts)
	}

	return out
}

// semanticPermissions answers Check from the stubbed decision tables; an
// unlisted resource is the zero Decision, Denied.
type semanticPermissions struct {
	user      accesstypes.User
	decisions map[accesstypes.Permission]accesstypes.Decisions
}

func (s *semanticPermissions) Check(_ context.Context, _ accesstypes.Environment, _ accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	out := make(accesstypes.Decisions, len(resources))
	for _, res := range resources {
		out[res] = s.decisions[perm][res]
	}

	return out, nil
}

func (*semanticPermissions) PermissionDigest(context.Context, accesstypes.Scope) (accesstypes.PermissionDigest, error) {
	return accesstypes.PermissionDigest{}, nil
}

func (*semanticPermissions) Domains(context.Context) ([]accesstypes.Domain, error) {
	return nil, nil
}

func (s *semanticPermissions) User() accesstypes.User {
	return s.user
}

// permits evaluates one decision over an image the way the read rules and the
// capability envelope read it: Granted permits, Denied refuses, Conditional
// permits when its condition's fail-open bound (post-image terms assumed
// TRUE) evaluates TRUE.
func permits(t *testing.T, res accesstypes.Resource, decision accesstypes.Decision, image *conditiontest.Image) bool {
	t.Helper()

	switch {
	case decision.IsGranted():
		return true
	case decision.IsConditional():
		expr, err := conditionalExpr(res, decision)
		if err != nil {
			t.Fatalf("conditionalExpr(%s) error = %v", res, err)
		}
		truth, err := conditiontest.Evaluate(condition.WithoutPostImage(expr), image)
		if err != nil {
			t.Fatalf("Evaluate(%s) error = %v", expr.String(), err)
		}

		return truth.Permits()
	default:
		return false
	}
}

// semanticTally counts what the run compared, reported at the end so a reader
// can see the differential exercised every answer it claims to.
type semanticTally struct {
	rows, maskedCells, visibleCells      int
	sortShapes, filterShapes             int
	updateGroups, insertGroups           int
	deleteGroups, forbiddenGates         int
	capabilityUpdates, capabilityDeletes int
}

// semanticHarness holds what every case shares: the client, the collection,
// the sets, the list decoder, the pools, and the tally.
type semanticHarness struct {
	tally      semanticTally
	client     *spanner.Client
	collection *GeneratedCollection
	listSet    *Set[semanticParcel]
	patchSet   *Set[semanticParcel]
	decoder    *QueryDecoder[semanticParcel, semanticListRequest]
	pools      *semanticPools
	readVocab  conditiontest.Vocabulary
	writeVocab conditiontest.Vocabulary
}

func newSemanticHarness(t *testing.T, client *spanner.Client) *semanticHarness {
	t.Helper()

	collection := semanticCollection(t)
	listSet, err := NewSet[semanticParcel, semanticListRequest](accesstypes.List)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	patchSet, err := NewSet[semanticParcel, semanticPatchRequest](accesstypes.Create, accesstypes.Update, accesstypes.Delete)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	decoder, err := NewQueryDecoder[semanticParcel, semanticListRequest](listSet)
	if err != nil {
		t.Fatalf("NewQueryDecoder() error = %v", err)
	}
	decoder.collection = collection
	decoder.WithPaging(Paging{DefaultLimit: 2 * semanticRowsPerCase})

	return &semanticHarness{
		client:     client,
		collection: collection,
		listSet:    listSet,
		patchSet:   patchSet,
		decoder:    decoder,
		pools:      newSemanticPools(t),
		readVocab:  semanticVocabulary(t, collection, false),
		writeVocab: semanticVocabulary(t, collection, true),
	}
}

// semanticCase is one drawn case with its decisions and its fixed
// projection: every grant-bearing field some read grant covers under a
// condition that did not fold FALSE (a Denied field is refused at the gate,
// which is Series A's proof, not this one).
type semanticCase struct {
	rng       *rand.Rand
	world     *semanticWorld
	policy    *semanticPolicy
	perms     *semanticPermissions
	decisions map[accesstypes.Permission]accesstypes.Decisions
	projected []semanticField
}

// report prefixes a failure with everything needed to reproduce it.
func (c *semanticCase) report() string {
	w := c.world

	return fmt.Sprintf("seed %d case %d: depot %s, requester %s, now %s, zone %s\npolicy:\n%s",
		semanticSeed, w.index, w.depot, w.requester, w.now.Format(time.RFC3339), w.zone, c.policy)
}

// TestSemanticDifferential runs the differential over the seeded cases, each
// in its own partition of one emulator database.
func TestSemanticDifferential(t *testing.T) {
	t.Parallel()

	container := spannerEmulator(t)
	ctx := context.Background()
	db, err := container.CreateDatabase(ctx, "semantic-differential")
	if err != nil {
		t.Fatalf("initiator.SpannerContainer.CreateDatabase() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.DropDatabase(context.Background()); err != nil {
			t.Error(err)
		}
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.MigrateUp("file://testdata/semantic/schema"); err != nil {
		t.Fatalf("initiator.SpannerDB.MigrateUp() error = %v", err)
	}

	// Cases run one at a time and each removes its rows when done: the
	// emulator evaluates a two-hop correlated subquery by scanning the
	// related tables for every row of the checked one, so a statement's cost
	// grows with everything written before it, and a hundred worlds at once
	// take it minutes per read. One world at a time keeps a case under a
	// second; the partition predicate still isolates the case's rows, and the
	// requester's membership in another partition still has to be excluded.
	h := newSemanticHarness(t, db.Client)
	for i := range semanticCases {
		c := h.drawCase(t, i)
		if _, err := h.client.Apply(ctx, c.world.mutations()); err != nil {
			t.Fatalf("%s\nspanner.Client.Apply() error = %v", c.report(), err)
		}

		h.checkRead(t, c)
		h.checkWrites(t, c)

		if _, err := h.client.Apply(ctx, c.world.deletions()); err != nil {
			t.Fatalf("%s\nspanner.Client.Apply(deletions) error = %v", c.report(), err)
		}
	}

	t.Logf("compared %+v", h.tally)
}

// drawCase draws the world, the policy, and the decisions for one case.
func (h *semanticHarness) drawCase(t *testing.T, index uint64) *semanticCase {
	t.Helper()

	rng := rand.New(rand.NewPCG(semanticSeed, index))
	world := genWorld(rng, h.pools, index)
	readGen := conditiontest.New(rng, &h.readVocab)
	writeGen := conditiontest.New(rng, &h.writeVocab)
	policy := genPolicy(rng, h.pools, readGen, writeGen)

	facts := world.facts()
	decisions := map[accesstypes.Permission]accesstypes.Decisions{
		accesstypes.List:   policy.decisions(t, accesstypes.List, h.listSet, facts),
		accesstypes.Update: policy.decisions(t, accesstypes.Update, h.patchSet, facts),
		accesstypes.Create: policy.decisions(t, accesstypes.Create, h.patchSet, facts),
		accesstypes.Delete: policy.decisions(t, accesstypes.Delete, h.patchSet, facts),
	}

	c := &semanticCase{
		rng:       rng,
		world:     world,
		policy:    policy,
		perms:     &semanticPermissions{user: accesstypes.User(world.requester), decisions: decisions},
		decisions: decisions,
	}
	for _, f := range semanticFields {
		if !decisions[accesstypes.List][h.listSet.Resource(f.field)].IsDenied() {
			c.projected = append(c.projected, f)
		}
	}
	if len(c.projected) == 0 {
		t.Fatalf("%s\nevery read grant folded FALSE; the case projects nothing", c.report())
	}

	return c
}

// semanticReadShape is the case's one query shape over a conditional column:
// a sort in either direction, or an isnull / isnotnull filter.
type semanticReadShape struct {
	field   semanticField
	sort    bool
	desc    bool
	notNull bool
}

func (s *semanticReadShape) query(projected []semanticField) url.Values {
	names := make([]string, 0, len(projected)+1)
	names = append(names, "id")
	for _, f := range projected {
		names = append(names, f.json)
	}
	values := url.Values{}
	values.Set(columnsParam, strings.Join(names, ","))
	values.Set(capabilitiesParam, string(accesstypes.Update)+","+string(accesstypes.Delete))
	switch {
	case s.sort && s.desc:
		values.Set(sortParam, s.field.json+":desc")
	case s.sort:
		values.Set(sortParam, s.field.json)
	case s.notNull:
		values.Set(filterParam, s.field.json+":"+isnotnullStr)
	default:
		values.Set(filterParam, s.field.json+":"+isnullStr)
	}

	return values
}

// drawReadShape prefers a conditionally granted field, so the sort or filter
// runs over the visible projection.
func (c *semanticCase) drawReadShape(set *Set[semanticParcel]) *semanticReadShape {
	var conditional []semanticField
	for _, f := range c.projected {
		if c.decisions[accesstypes.List][set.Resource(f.field)].IsConditional() {
			conditional = append(conditional, f)
		}
	}
	shape := &semanticReadShape{field: draw(c.rng, c.projected)}
	if len(conditional) > 0 {
		shape.field = draw(c.rng, conditional)
	}
	switch c.rng.IntN(4) {
	case 0:
		shape.sort = true
	case 1:
		shape.sort = true
		shape.desc = true
	case 2:
		shape.notNull = true
	}

	return shape
}

// semanticExpected is the evaluator's answer for one surviving row.
type semanticExpected struct {
	parcel  *semanticParcel
	visible map[accesstypes.Field]bool
	key     any
	update  []string
	delete  bool
}

// expectRead computes the surviving rows in order, each with its visible
// cells and capability answers, from the decisions and the evaluator.
func (h *semanticHarness) expectRead(t *testing.T, c *semanticCase, shape *semanticReadShape) []*semanticExpected {
	t.Helper()

	var expected []*semanticExpected
	for _, p := range c.world.parcels {
		image := c.world.image(p, nil)
		e := &semanticExpected{parcel: p, visible: make(map[accesstypes.Field]bool, len(c.projected))}
		survives := false
		for _, f := range c.projected {
			res := h.listSet.Resource(f.field)
			e.visible[f.field] = permits(t, res, c.decisions[accesstypes.List][res], image)
			survives = survives || e.visible[f.field]
			if permits(t, res, c.decisions[accesstypes.Update][h.patchSet.Resource(f.field)], image) {
				e.update = append(e.update, f.json)
			}
		}
		if !survives {
			continue
		}
		e.delete = permits(t, semanticResource, c.decisions[accesstypes.Delete][semanticResource], image)
		if e.visible[shape.field.field] {
			e.key = semanticValue(semanticFieldValue(p, shape.field.field))
		}
		if !shape.sort && (e.key == nil) == shape.notNull {
			continue
		}
		expected = append(expected, e)
	}

	// Spanner orders NULL first ascending and last descending; the key
	// breaks ties.
	sort.SliceStable(expected, func(i, j int) bool {
		if shape.sort {
			order := compareSemantic(expected[i].key, expected[j].key)
			if shape.desc {
				order = -order
			}
			if order != 0 {
				return order < 0
			}
		}

		return expected[i].parcel.ID.String() < expected[j].parcel.ID.String()
	})

	return expected
}

// checkRead runs the case's read and compares it with the evaluator.
func (h *semanticHarness) checkRead(t *testing.T, c *semanticCase) {
	t.Helper()

	shape := c.drawReadShape(h.listSet)
	target := "/?" + shape.query(c.projected).Encode()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)
	qSet, err := h.decoder.Decode(req, c.perms, c.world.scope())
	if err != nil {
		t.Fatalf("%s\nread %s: Decode() error = %v", c.report(), target, err)
	}
	qSet.env = c.world.env()

	var rows []*Row[semanticParcel]
	for row, err := range qSet.List(t.Context(), NewSpannerClient(h.client)) {
		if err != nil {
			t.Fatalf("%s\nread %s: List() error = %v", c.report(), target, err)
		}
		rows = append(rows, row)
	}

	statement := func() string {
		stmt, err := qSet.stmt(SpannerDBType)
		if err != nil {
			return err.Error()
		}

		return fmt.Sprintf("%s\nparams: %v", normalizeSQL(stmt.SQL), stmt.Params)
	}
	fail := func(format string, args ...any) {
		t.Helper()
		t.Fatalf("%s\nread %s\n%s\n%s", c.report(), target, statement(), fmt.Sprintf(format, args...))
	}

	if shape.sort {
		h.tally.sortShapes++
	} else {
		h.tally.filterShapes++
	}

	expected := h.expectRead(t, c, shape)
	gotIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		gotIDs = append(gotIDs, row.Data.ID.String())
	}
	wantIDs := make([]string, 0, len(expected))
	for _, e := range expected {
		wantIDs = append(wantIDs, e.parcel.ID.String())
	}
	if !slices.Equal(gotIDs, wantIDs) {
		fail("surviving rows (in order) diverged:\n got %v\nwant %v", gotIDs, wantIDs)
	}

	parcelType := reflect.TypeFor[semanticParcel]()
	h.tally.rows += len(rows)
	for i, row := range rows {
		e := expected[i]
		for _, f := range c.projected {
			field, _ := parcelType.FieldByName(string(f.field))
			got := semanticValue(reflect.ValueOf(row.Data).FieldByName(string(f.field)).Interface())
			if e.visible[f.field] {
				h.tally.visibleCells++
				want := semanticValue(semanticFieldValue(e.parcel, f.field))
				if row.Masked(f.json) {
					fail("row %s: %s is masked, the evaluator finds its condition TRUE", e.parcel.ID, f.json)
				}
				if compareSemantic(got, want) != 0 {
					fail("row %s: visible %s = %v, want %v", e.parcel.ID, f.json, got, want)
				}

				continue
			}
			h.tally.maskedCells++
			if !row.Masked(f.json) {
				fail("row %s: %s is not masked, the evaluator finds its condition not TRUE", e.parcel.ID, f.json)
			}
			if filler := semanticValue(maskFiller(field.Type)); compareSemantic(got, filler) != 0 {
				fail("row %s: masked %s = %v, want the filler %v", e.parcel.ID, f.json, got, filler)
			}
		}

		caps := row.Capabilities()
		gotUpdate, _ := caps[accesstypes.Update].([]string)
		if !slices.Equal(gotUpdate, e.update) {
			fail("row %s: Update capability = %v, want %v", e.parcel.ID, gotUpdate, e.update)
		}
		h.tally.capabilityUpdates += len(gotUpdate)
		gotDelete, _ := caps[accesstypes.Delete].(bool)
		if gotDelete != e.delete {
			fail("row %s: Delete capability = %v, want %v", e.parcel.ID, gotDelete, e.delete)
		}
		if gotDelete {
			h.tally.capabilityDeletes++
		}
	}
}

// proposedValue draws a proposed value for a field in the struct's own type.
func (h *semanticHarness) proposedValue(rng *rand.Rand, w *semanticWorld, field accesstypes.Field) any {
	pools := h.pools
	switch field {
	case "Label":
		return draw(rng, pools.strings)
	case "Note":
		return maybe(rng, pools.strings)
	case "Weight":
		return draw(rng, pools.ints)
	case "Pieces":
		return maybe(rng, pools.ints)
	case "Ratio":
		return draw(rng, pools.floats)
	case "Density":
		return maybe(rng, pools.floats)
	case "Price":
		return draw(rng, pools.numerics)
	case "Fee":
		if d := maybe(rng, pools.numerics); d != nil {
			return decimal.NewNullDecimal(*d)
		}

		return decimal.NullDecimal{}
	case "Fragile":
		return rng.IntN(2) == 0
	case "Insured":
		return maybe(rng, []bool{true, false})
	case "ShippedAt":
		return draw(rng, pools.times)
	case "DeliveredAt":
		return maybe(rng, pools.times)
	case "ShipDate":
		return draw(rng, pools.dates)
	case "DueDate":
		return maybe(rng, pools.dates)
	case "CarrierID":
		return maybe(rng, slices.Sorted(maps.Keys(w.carriers)))
	case "RouteID":
		return maybe(rng, slices.Sorted(maps.Keys(w.routes)))
	default:
		panic("proposedValue: unknown field " + string(field))
	}
}

// newPatch builds an enforced patch set of one type over the case's
// partition, stamped with the case's environment and the collection.
func (h *semanticHarness) newPatch(c *semanticCase, patchType PatchType, perm accesstypes.Permission, id ccc.UUID) *PatchSet[semanticParcel] {
	ps := NewPatchSet(NewMetadata[semanticParcel]())
	ps.querySet.env = c.world.env()
	ps.querySet.collection = h.collection
	ps.EnableUserPermissionEnforcement(h.patchSet, c.perms, c.world.scope(), perm)
	ps.SetPatchType(patchType)
	ps.SetKey("ID", id)

	return ps
}

// checkGroups renders and runs a mutation's check-SELECT and compares each
// group boolean with the evaluator over the image. forbidden says whether the
// static gate must refuse the mutation before any SQL.
func (h *semanticHarness) checkGroups(t *testing.T, c *semanticCase, what string, ps *PatchSet[semanticParcel], forbidden bool, image *conditiontest.Image) {
	t.Helper()

	err := ps.checkPermissions(t.Context(), SpannerDBType)
	if forbidden {
		if err == nil || !strings.Contains(err.Error(), "does not have") {
			t.Fatalf("%s\n%s: the static gate admitted a mutation touching a Denied column (error = %v)", c.report(), what, err)
		}
		h.tally.forbiddenGates++

		return
	}
	if err != nil {
		t.Fatalf("%s\n%s: checkPermissions() error = %v", c.report(), what, err)
	}

	groups, err := ps.writeConditionGroups()
	if err != nil {
		t.Fatalf("%s\n%s: writeConditionGroups() error = %v", c.report(), what, err)
	}
	if len(groups) == 0 {
		return
	}
	tenancy, err := ps.mutationTenancy()
	if err != nil {
		t.Fatalf("%s\n%s: mutationTenancy() error = %v", c.report(), what, err)
	}
	stmt, err := ps.writeCheckStatement(SpannerDBType, groups, tenancy)
	if err != nil {
		t.Fatalf("%s\n%s: writeCheckStatement() error = %v", c.report(), what, err)
	}

	it := h.client.Single().Query(t.Context(), stmt.SpannerStatement())
	defer it.Stop()
	row, err := it.Next()
	if err != nil {
		t.Fatalf("%s\n%s: check statement failed: %v\n%s\nparams: %v", c.report(), what, err, stmt.SQL, stmt.Params)
	}
	switch ps.patchType {
	case UpdatePatchType:
		h.tally.updateGroups += len(groups)
	case CreatePatchType:
		h.tally.insertGroups += len(groups)
	case DeletePatchType:
		h.tally.deleteGroups += len(groups)
	default:
	}
	for i, group := range groups {
		var passed spanner.NullBool
		if err := row.Column(i, &passed); err != nil {
			t.Fatalf("%s\n%s: spanner.Row.Column(%d) error = %v", c.report(), what, i, err)
		}
		got := passed.Valid && passed.Bool
		truth, err := conditiontest.Evaluate(group.expr, image)
		if err != nil {
			t.Fatalf("%s\n%s: Evaluate(%s) error = %v", c.report(), what, group.expr.String(), err)
		}
		if got != truth.Permits() {
			t.Fatalf("%s\n%s: group %d (%s) = %v (NULL read as false), the evaluator finds %s\n%s\nparams: %v",
				c.report(), what, i+1, group.source, got, truth, stmt.SQL, stmt.Params)
		}
	}
}

// checkWrites samples rows and, for each, runs an update touching a random
// subset of columns, an insert with every column set, and a delete.
func (h *semanticHarness) checkWrites(t *testing.T, c *semanticCase) {
	t.Helper()

	w := c.world
	sample := slices.Clone(w.parcels)
	c.rng.Shuffle(len(sample), func(i, j int) {
		sample[i], sample[j] = sample[j], sample[i]
	})
	for _, p := range sample[:semanticWriteSample] {
		h.checkUpdate(t, c, p)
		h.checkInsert(t, c)
		h.checkDelete(t, c, p)
	}
}

func (h *semanticHarness) checkUpdate(t *testing.T, c *semanticCase, p *semanticParcel) {
	t.Helper()

	touched := slices.Clone(semanticFields)
	c.rng.Shuffle(len(touched), func(i, j int) {
		touched[i], touched[j] = touched[j], touched[i]
	})
	touched = touched[:1+c.rng.IntN(len(touched))]

	ps := h.newPatch(c, UpdatePatchType, accesstypes.Update, p.ID)
	post := make(map[string]any, len(touched))
	forbidden := c.decisions[accesstypes.Update][semanticResource].IsDenied()
	for _, f := range touched {
		value := h.proposedValue(c.rng, c.world, f.field)
		ps.Set(f.field, value)
		if i := slices.IndexFunc(semanticColumnAttributes, func(a semanticAttribute) bool {
			return a.field == f.field
		}); i >= 0 {
			post[semanticColumnAttributes[i].name] = semanticValue(value)
		}
		if c.decisions[accesstypes.Update][h.patchSet.Resource(f.field)].IsDenied() {
			forbidden = true
		}
	}

	h.checkGroups(t, c, fmt.Sprintf("update %s touching %v", p.ID, touched), ps, forbidden, c.world.image(p, post))
}

func (h *semanticHarness) checkInsert(t *testing.T, c *semanticCase) {
	t.Helper()

	proposed := genParcel(c.rng, h.pools, c.world, slices.Sorted(maps.Keys(c.world.carriers)), slices.Sorted(maps.Keys(c.world.routes)))
	ps := h.newPatch(c, CreatePatchType, accesstypes.Create, proposed.ID)
	ps.Set("Depot", proposed.Depot)
	forbidden := c.decisions[accesstypes.Create][semanticResource].IsDenied()
	for _, f := range semanticFields {
		ps.Set(f.field, semanticFieldValue(proposed, f.field))
		if c.decisions[accesstypes.Create][h.patchSet.Resource(f.field)].IsDenied() {
			forbidden = true
		}
	}

	// An insert has one image, read unqualified: the proposed row itself,
	// its join paths resolved through the proposed keys.
	h.checkGroups(t, c, fmt.Sprintf("insert %s", proposed.ID), ps, forbidden, c.world.image(proposed, nil))
}

func (h *semanticHarness) checkDelete(t *testing.T, c *semanticCase, p *semanticParcel) {
	t.Helper()

	ps := h.newPatch(c, DeletePatchType, accesstypes.Delete, p.ID)
	forbidden := c.decisions[accesstypes.Delete][semanticResource].IsDenied()
	h.checkGroups(t, c, fmt.Sprintf("delete %s", p.ID), ps, forbidden, c.world.image(p, nil))
}
