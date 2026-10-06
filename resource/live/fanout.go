package live

import (
	"context"
	"slices"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/go-playground/errors/v5"
)

// Fanout computes the change documents one publish writes, per principal, from the
// record: for each resource touched under BulkThreshold rows, a list document for every
// subscriber of the resource's list in the request's domain and a row document for
// every subscriber of each touched row; above the threshold, one lookup of the
// resource's subscribers and a resource document for each user subscribed to any of its
// rows or to its list in the request's domain, with no per-row lookups. A user
// subscribed through several tabs, or to a row and the list both, gets each distinct
// document once; a row written and then deleted reports deleted. Expired subscriptions
// are the record's to leave out. Every publisher implementation writes what Fanout
// returns, so the shape of a change set is decided here once.
func Fanout(ctx context.Context, record SubscriptionRecord, domain accesstypes.Domain, touched map[accesstypes.Resource][]resource.RowChange) (Changes, error) {
	changes := make(Changes)
	add := func(subs []Subscription, doc ChangeDocument) {
		for _, sub := range subs {
			changes.add(sub.Principal, doc)
		}
	}

	resources := slices.Sorted(func(yield func(accesstypes.Resource) bool) {
		for res := range touched {
			if !yield(res) {
				return
			}
		}
	})
	for _, res := range resources {
		rows := touched[res]
		if len(rows) == 0 {
			continue
		}
		if len(rows) > BulkThreshold {
			subs, err := record.SubscribersOfResource(ctx, res)
			if err != nil {
				return nil, errors.Wrap(err, "live.SubscriptionRecord.SubscribersOfResource()")
			}
			for _, sub := range subs {
				if sub.IsRow() || sub.Domain == domain {
					changes.add(sub.Principal, ChangeDocument{Kind: ResourceChange, Resource: res})
				}
			}

			continue
		}

		listSubs, err := record.SubscribersOfList(ctx, res, domain)
		if err != nil {
			return nil, errors.Wrap(err, "live.SubscriptionRecord.SubscribersOfList()")
		}
		add(listSubs, ChangeDocument{Kind: ListChange, Resource: res, Domain: domain})
		for _, row := range rows {
			rowSubs, err := record.SubscribersOfRow(ctx, res, row.Key)
			if err != nil {
				return nil, errors.Wrap(err, "live.SubscriptionRecord.SubscribersOfRow()")
			}
			add(rowSubs, ChangeDocument{Kind: RowChange, Resource: res, Key: row.Key, Deleted: row.Deleted})
		}
	}

	return changes, nil
}

// add appends doc to principal's documents, once per target; a later write to the same
// target takes the later deleted flag.
func (c Changes) add(principal string, doc ChangeDocument) {
	docs := c[principal]
	at := slices.IndexFunc(docs, func(d ChangeDocument) bool {
		return d.target() == doc.target()
	})
	if at >= 0 {
		docs[at].Deleted = doc.Deleted

		return
	}
	c[principal] = append(docs, doc)
}

// Live filters subs to the ones whose expiry is after now: what every record
// implementation answers from a lookup.
func Live(subs []Subscription, now time.Time) []Subscription {
	live := make([]Subscription, 0, len(subs))
	for _, sub := range subs {
		if sub.Expiry.After(now) {
			live = append(live, sub)
		}
	}

	return live
}
