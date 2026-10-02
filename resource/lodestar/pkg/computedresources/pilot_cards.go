package computedresources

import (
	"context"
	"iter"
	"slices"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

type (
	// PilotCard is the caller's own standing: the one row that is "mine", folded from
	// the subject attribute tables (Pilots, PilotCertifications, SquadronMemberships)
	// for the identity the permission check ran as. It is the CALLER-SCOPED READ: List
	// yields exactly one row, chosen by QuerySet.User() — the viewed person under a
	// view-as session, the real actor under an act-as-role session — so the header
	// card changes when the Overseer views as a Cadet. A question with no write is a
	// computed resource, not an RPC. The keyed read route is suppressed: there is no
	// key to give but one's own.
	//
	// Commendations is the card's one gated field: how many citations the commendations
	// desk holds on the pilot's record, behind the same flag as the desk
	// (@feature(Commendations) on the field). While the flag is off the field is not on
	// the card: a request naming it in columns= is refused as an unknown field, the
	// list leaves it out, and the digest leaves PilotCards.commendations out, so the
	// browser's card draws nothing for it. When the flag is on the field is served under
	// the crew's ordinary List grant, which has carried it since the feature shipped.
	//
	// Demonstrates: computed.user, @computed, @suppress, @feature.field.
	//
	// @computed
	// @suppress(readHandler)
	// @order(DisplayName asc)
	// @page(default: 25, max: 200)
	PilotCard struct {
		UserID         string          `spanner:"UserId"` // @primarykey
		DisplayName    string          `spanner:"DisplayName"`
		Clearance      int64           `spanner:"Clearance"`
		FeeLimit       decimal.Decimal `spanner:"FeeLimit"`
		Certifications []string        `spanner:"Certifications"`
		Squadrons      []string        `spanner:"Squadrons"`
		// @feature(Commendations)
		Commendations int64 `spanner:"Commendations"`
	}
)

// Resource implements resource.Resourcer.
func (PilotCard) Resource() accesstypes.Resource {
	return "PilotCards"
}

// ListPilotCard yields the caller's card, or nothing when the caller has no pilot
// record (a client, a droid).
func ListPilotCard(ctx context.Context, qSet *resource.QuerySet[PilotCard], client resource.Client, _ *Client) iter.Seq2[*PilotCard, error] {
	return func(yield func(*PilotCard, error) bool) {
		card, err := pilotCard(ctx, client, qSet.User())
		if err != nil {
			yield(nil, err)

			return
		}
		if card != nil {
			yield(card, nil)
		}
	}
}

// pilotCard folds the subject attribute tables for one user.
func pilotCard(ctx context.Context, client resource.Client, user accesstypes.User) (*PilotCard, error) {
	var card *PilotCard
	var pilotID ccc.UUID
	pilots := resources.NewPilotQuery().
		AddColumns(resources.NewPilotColumns().All()).
		Where(resources.NewPilotQueryClause().UserID().Equal(string(user)))
	for row, err := range pilots.List(ctx, client) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.PilotQuery.List()")
		}
		pilotID = row.Data.ID
		card = &PilotCard{UserID: row.Data.UserID, DisplayName: row.Data.DisplayName, Clearance: row.Data.Clearance, FeeLimit: row.Data.FeeLimit}
	}
	if card == nil {
		return nil, nil
	}

	// The citations on the pilot's record, counted off the desk's index whether or not
	// the commendations flag is on: the generated handler leaves the field out of the
	// card while the flag is off, and the count is one indexed read.
	commendations := resources.NewCommendationQuery().
		AddColumns(resources.NewCommendationColumns().ID()).
		Where(resources.NewCommendationQueryClause().PilotID().Equal(pilotID))
	for _, err := range commendations.List(ctx, client) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.CommendationQuery.List()")
		}
		card.Commendations++
	}

	certifications := resources.NewPilotCertificationQuery().
		AddColumns(resources.NewPilotCertificationColumns().All()).
		Where(resources.NewPilotCertificationQueryClause().UserID().Equal(string(user)))
	for row, err := range certifications.List(ctx, client) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.PilotCertificationQuery.List()")
		}
		card.Certifications = append(card.Certifications, row.Data.CertificationID)
	}
	slices.Sort(card.Certifications)

	names := make(map[string]string)
	for row, err := range resources.NewSquadronQuery().AddColumns(resources.NewSquadronColumns().All()).List(ctx, client) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.SquadronQuery.List()")
		}
		names[row.Data.ID.String()] = row.Data.Name
	}
	memberships := resources.NewSquadronMembershipQuery().
		AddColumns(resources.NewSquadronMembershipColumns().All()).
		Where(resources.NewSquadronMembershipQueryClause().UserID().Equal(string(user)))
	for row, err := range memberships.List(ctx, client) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.SquadronMembershipQuery.List()")
		}
		card.Squadrons = append(card.Squadrons, names[row.Data.SquadronID.String()])
	}
	slices.Sort(card.Squadrons)

	return card, nil
}
