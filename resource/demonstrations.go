package resource

import "sort"

// Demonstration is one capability of the resource package a demonstration application
// proves. The key is the vocabulary an application declares against: the annotation,
// option, grammar function, or API name where one exists, a short coined slug where none
// does. A demonstration application ends the doc comment of every struct, method, test,
// component, and persona row that proves a key with a "Demonstrates: key, key." paragraph,
// and a test in the application fails when a key here has no declaration in code or in a
// test, when a declaration names a key not here, or when a README example link points at
// a file that does not declare the key its row documents. The same test writes the
// generated key-to-files table (lodestar/DEMONSTRATIONS.md).
type Demonstration struct {
	Key         string
	Description string
}

// Demonstrations is the registry: every capability a demonstration application is
// expected to prove, seeded from the Lodestar design plan's coverage matrix.
var Demonstrations = []Demonstration{
	// Tenancy.
	{"tenancy.tenant-record", "The tenant-record pattern: a table whose rows are the permission domains, read into a roster at startup; WithDomainRoute derives the route parameter from its key."},
	{"tenancy.concealed", "WithConcealedDomains: a domain the caller holds no grant in answers exactly like one that does not exist."},
	{"tenancy.user-domains", "The generated user-domains endpoint: the domains where the session holds at least one grant, on the same foothold predicate as concealed tenancy."},
	{"star-chart", "A picker drawn from user-domains, with a labeled bypass of the digest-first rule so a concealed refusal can be shown in a browser."},
	{"@domain", "The bare @domain column: the tenant key on the row itself, stamped from the request on create."},
	{"@domain.join-path", "@domain(via: ...): tenancy resolved one or more hops away through foreign keys."},
	{"fail-closed.bare-resource", "A resource carrying only the mandatory @domain: a resource-only grant exposes nothing beyond the primary key."},

	// Attributes and subjects.
	{"@attribute", "@attribute(name) on a column: the vocabulary a grant condition compares."},
	{"@attribute.join-path", "@attribute(name, via: ...) reaching a column one hop away within the tenant."},
	{"@attribute.join-path-global", "A join-path attribute from a tenant-scoped table into a global table."},
	{"@attribute.bool", "A boolean attribute compared with = true."},
	{"@attribute.date", "A date attribute compared against a date literal."},
	{"@attribute.decimal", "A decimal attribute compared against numbers and subject values."},
	{"@attribute.timestamp", "A timestamp attribute compared against now."},
	{"@attribute.nullable-fk", "A nullable foreign-key attribute tested with IS NULL inside an OR."},
	{"@subjectSet.domain", "@subjectSet on a tenant-scoped anchor: the subject's set is partitioned per tenant."},
	{"@subjectSet.global", "@subjectSet on a global anchor: earned once, valid in every tenant, deliberately unfiltered."},
	{"@subjectSet.dotted-value", "@subjectSet(name, value: A.B): a set whose value continues through the anchor's foreign key."},
	{"@subjectValue", "@subjectValue(name, value: Column): a scalar attribute of the requesting user."},
	{"@subjectValue.two-per-anchor", "Two subject values bound on one unique-indexed user column."},
	{"@subjectValue.second-anchor", "A second @subjectValue anchor, so a condition can compare a foreign key to the subject's company."},
	{"condition.subject-scalar", "subject as a scalar compared to a user column."},
	{"condition.now", "now as an operand, on either side, folded at decision time; a row-free condition on an Execute grant."},
	{"condition.time-of-day", "timeOfDay(now, zone) compared against 'HH:MM' literals, folded in the engine and never rendered to SQL."},
	{"condition.day-of-week", "dayOfWeek(now, zone) compared against day names with =, !=, and [NOT] IN."},
	{"condition.local-zone", "The bare word local as a zone, wired by the application through SetLocalZone."},
	{"condition.old-vs-new", "new.attr <op> attr on an Update: the post-image against the same row's pre-image."},
	{"condition.not-in", "NOT IN over a string list, on a state binding."},
	{"condition.prefix-not", "Prefix NOT over a parenthesised OR."},
	{"write-grouping", "Two conditional grants for one permission on one resource in one role: a PATCH touching both field sets is checked as AND across groups, and a denial names the failing group."},

	// Workflows.
	{"@state", "@state(default: ...) on the status column: the state is structurally unwritable from the wire."},
	{"@stateRoot", "@stateRoot(Root) on a member's anchoring foreign key: the uniform state binding synthesized one hop down."},
	{"@stateRoot.two-hop", "A member two hops from the root: the chain resolver composes the hops."},
	{"@transition", "@transition(Root, from: ..., to: ...): the declared edge is the whole legality rule."},
	{"@transition.multi-from", "A transition with several source states."},
	{"@transition.loop", "A state entered and left again: the hold/resume loop, the failed-test loop."},
	{"@transition.join-path-root", "A transition whose root's tenancy is a join path: tenancy verified by the gate's check-SELECT."},
	{"@target", "@target(Root) alone: the plain located-row form of a method that moves no state."},
	{"workflow.state-table", "A status table whose values are the workflow's states; mutation grants against it are structurally rejected."},
	{"workflow.dot", "The generated DOT graph of each workflow: facts drawn, policy not."},
	{"workflow.ts-constant", "The generated TypeScript Workflows constant the browser draws the graph from."},
	{"execute-condition", "A row condition on an Execute grant, evaluated against the located row; the refusal names the method and row only."},
	{"capability-envelope", "capabilities=Execute,Create,Update,Delete on a read: the per-row answers a page renders its affordances from."},
	{"create-under-parent", "capabilities=Create on a parent lists the workflow members a row admits creating beneath it."},
	{"touch", "output_only_update_fn's generated Touch: a row bumps its stamp with no field changed."},
	{"transition-owned-timestamp", "A timestamp written explicitly by the transition that owns the event, not by an update function."},
	{"test-suffixed-method", "A method whose snake-cased name ends in _test lives in a _rpc-marked file."},

	// Fields.
	{"cell-masking", "A field a conditional grant does not cover on a row arrives as an absent key."},
	{"pii", "conditions:\"pii\": a field flagged in metadata and refused in URL filters."},
	{"pii.filter-placement", "A PII filter is refused in the URL and accepted in the request body."},
	{"input_only", "A field accepted on mutations and never serialized back."},
	{"output_only", "A field the wire can never write."},
	{"default_create_fn", "A server-side default computed at create time."},
	{"immutable", "A field that can be created but never updated."},
	{"output_only_update_fn", "A mechanical stamp written on every update."},
	{"@defaultsCreateType", "A defaults type run inside the create transaction."},
	{"@validateCreateType", "A validator type run inside the create transaction."},
	{"@defaultsUpdateType", "A defaults type run inside the update transaction."},
	{"@validateUpdateType", "A validator type run inside the update transaction."},
	{"allow_filter", "An unindexed column made filterable."},
	{"index.tenant-second", "A column directly after the tenant column in an index key is indexed on a bare @domain resource: the list binds the tenant by equality, so a filter on the column alone seeks the index, and the column carries index:\"true\" and filterable: 'always'."},
	{"index.trailing-key", "A trailing key column with nothing bound before it is not indexed: a filter on it alone would scan the index, so the column carries no index tag and no filterable metadata, and a filter naming it alone is refused."},
	{"filter.typed-values", "A filter value typed by its column: a decimal binds as NUMERIC, a non-number is a 400."},
	{"filter.validated-at-decode", "A filter validated with the rest of the request, before permission checks and before any body runs."},
	{"create-form-narrowing", "The digest's field-level Create entries decide which inputs a form renders."},

	// Structure.
	{"interleaved-table", "An interleaved child table."},
	{"compound-key", "A compound primary key."},
	{"client-supplied-key", "A create that supplies its own key."},
	{"consolidation.exclusion", "A resource excluded from the consolidated handler keeps its standalone PATCH surface."},
	{"consolidation.batch", "The consolidated endpoint: a cross-resource batch in one transaction with a closed operation vocabulary."},
	{"commit.referential-refusal", "A commit Spanner refuses for a referential reason (a delete of a row other rows still reference, a write naming a referenced row that does not exist, a required column left empty) answers 409 with a message naming only the resources the transaction buffered, decided on the gRPC code alone; a child-and-parent delete in one transaction still succeeds in either order."},
	{"commit.constraint-refusal", "A commit Spanner refuses for a duplicate key or unique-index value (409), a violated CHECK constraint (400), or an update of a row that does not exist (404) answers in the resource's name, decided on the gRPC code alone over the same record of buffered patches; the create validator's 400 for the same rule shows the two enforcement points side by side."},
	{"change-tracking", "TrackChanges: mutations write DataChangeEvents rows in the same transaction."},
	{"event-source", "The change event's source names the session, and the actor and role of an impersonated one."},
	{"@enumerate", "The field-scope @enumerate(Resource): the field holds another resource's identifier, and the generator alone says which resource its picker lists."},
	{"@enumerate.plain-column", "A field-scope @enumerate on a plain column with no foreign key: an identifier held from outside the schema names the resource its picker lists."},
	{"@enumerate.key-view", "A field-scope @enumerate on a foreign key naming a view over its target, keyed like it, that carries display columns the target lacks; the constraint stays the guard."},
	{"@enumerate.enum-table", "A field-scope @enumerate naming an enum table: the picker renders the generated values with no request and no List grant, as an inferred key into the table does."},
	{"@enumerate.computed", "A field-scope @enumerate naming a computed resource: the picker lists rows served from Go."},
	{"@enumerate.type", "The type-scope @enumerate(Table): generated constants for an enum table's values."},
	{"picker.read-disabled", "A picker over a resource with no read handler: the picked value's display resolves from the option list, and an unmatched id shows as itself."},
	{"picker.config-driven", "A config-driven page whose pickers list exactly the resources the generated metadata names, in the selected tenant; a persona without List on one sees the refusal."},
	{"@suppress", "@suppress(readHandler | patchHandler): a route not generated."},
	{"@virtual", "A virtual resource over an embedded subquery."},
	{"virtual.with-clause", "A virtual subquery with a WITH clause."},
	{"virtual.named-param", "A virtual subquery with a named parameter."},
	{"virtual.domain", "A tenant-scoped virtual resource over a view column."},
	{"@rowsOf", "The struct-scope @rowsOf(Table) on a view: the view declares its backing table, a create goes into the table, and the new row shows up in the view on the next list because the view's SQL reads that table."},
	{"@rowsOf.same-row", "A view keyed by its table's own key: a row opens the table's page, an edit patches the table, and a delete removes the row, while the view's other columns stay its own."},
	{"@rowsOf.association", "Two views over one association table, keyed by the table's compound key under its column names: the key rides in every list request, a row is deleted from the list through the table, and a create associates through the table's form."},
	{"list.row-route", "A list page's rowRoute names a field whose declared enumeration is the row's destination: the row opens that resource by the field's value, gated on Read of it; without one the row opens the write resource by its single key."},
	{"@computed", "A computed resource: rows from application query logic behind generated routes."},
	{"computed.domain", "A tenant-scoped computed resource."},
	{"computed.compound-key", "A computed resource with a compound key."},
	{"computed.conditional-grant", "A row-free conditional grant on a computed resource."},
	{"computed.fold", "A computed List answered by the handler's in-memory fold (Collect)."},
	{"computed.pushdown", "A computed List that takes filter, sort, and page into its own SQL."},
	{"computed.take-filter", "Filter().Take on a computed resource's filterable columns."},
	{"computed.take-sort", "TakeSort: the body yields rows in the total order."},
	{"computed.take-page", "TakePage: the body pages itself from the cursor's boundary."},
	{"computed.user", "QuerySet.User(): the identity the check ran as, for a caller-scoped computed read."},
	{"computed.scope", "QuerySet.Scope(): the partition the check ran in."},
	{"@manualAddResource", "A permission registered by hand for a hand-written route."},
	{"@manualAddResource.scope", "@manualAddResource(Perm, domain): the scope argument."},
	{"@manualAddResource.execute", "@manualAddResource(Execute): a manual Execute registration reaching the TypeScript Methods constants."},
	{"@manualAddResource.outlet", "A manual registration naming a non-default @outlet, filtered by ForOutlet."},
	{"hand-written-route", "A route the application writes itself, checking the registered permission fail-closed."},

	// Outlets and auth.
	{"outlet.shared", "@outlet(default, other): a struct served on two outlets."},
	{"outlet.exclusive", "@outlet(other) alone: no route on the default outlet."},
	{"outlet.isolation", "The generated router tests and the served suites prove the outlets' URL spaces are disjoint."},
	{"outlet.session", "WithRouterOutlet(..., Auth(...)) (ServesSessions() under a hand-written router): a second browser surface with its own permission routes."},
	{"GenerateRouter", "The generated router: each outlet declares Auth or APIKey and its WebApp, the generator emits the router with its middleware chain documented and proven, and the application's own routes compose in through Hooks."},
	{"outlet.api-key", "An API-key outlet bound to a service identity through the same permission checks."},
	{"machine-identity", "A service account holding roles with no login."},
	{"typescript.second-target", "GenerateTypescript(..., ForOutlet(...)): a second client filtered to an outlet, generated in the same run."},
	{"auth.password", "A PasswordAuth population whose roles the application owns."},
	{"auth.two-populations", "Two auth packages, each with its own tables, cookies, and permission store, each outlet bound to one."},
	{"auth.directory-roles", "An OIDC auth whose role membership is the directory's (RoleSync), with no role writer in the application."},
	{"auth.skipauth-directory", "The session library's skipAuth build simulating the directory from APP_USERNAME and APP_ROLES."},
	{"auth.login-refusal-code", "A refused login returns to the login page with a code in ?code=, never text: the page holds the sentence for each code and shows nothing for one it does not know."},

	// Paging.
	{"@order", "@order(Field asc|desc): a list's declared total order."},
	{"@page", "@page(default: N, max: M): a list's page sizes, carried into the descriptor."},
	{"paging.cursor", "A keyset cursor: pages positioned by the row the last page ended on."},
	{"paging.link-header", "The Link header's next and prev relations, followed exactly as issued."},
	{"paging.total-count", "count=true on a first page answers Total-Count."},
	{"paging.limit-all", "limit=all, legal only where no maximum is declared."},
	{"paging.offset-refused", "The offset parameter is refused."},
	{"paging.readability-rule", "A sort or filter on a field the caller is denied is refused naming the field."},
	{"paging.masked-sort", "A sort or filter on a conditionally granted field runs over the visible projection: masked cells are NULL."},
	{"paging.unselected-sort-key", "A sort key outside the columns projection, the primary key included, pages on the cursor's copy of the value: the statement selects it a second time under a reserved column, and the row data carries nothing extra."},
	{"masking.positional", "masking:\"positional\" on a field: its masked cells stay hidden, but the list orders, filters, and pages on the real column, so the page comes off the index and the field's rank is disclosed."},
	{"warning.concealing-key", "MigrateRoles and ValidateRoles warn where a role's conditional grant lands on a concealing field the resource orders by or admits as a sort or filter key, and the role's other grants leave the CASE in the query: that role's pages sort the partition."},
	{"warning.tenant-index", "Generation warns, naming the CREATE INDEX it wants, where a listed resource with a bare @domain and an @order has no index leading with the tenant column and then the order columns: every page would sort the tenant's partition."},
	{"warning.join-path-list", "Generation warns where a listed resource resolves its tenant through @domain(via: ...): its lists scan the whole table, every tenant, and no index on it changes that."},
	{"paging.nullable-sort", "A nullable sort column walked across the NULL boundary in both directions."},
	{"paging.survives-writes", "A walk that sees each row once across inserts and deletes before its position."},
	{"paging.sealed-cursor", "The cursor is a sealed token carrying nothing readable."},
	{"paging.descriptor-sizes", "The browser reads page sizes from the generated descriptor, never a literal."},
	{"list.server-paged", "The library's config-driven list is a server-paged consumer: one page at the descriptor's size, First, Previous, and Next by the server's cursors, the first page's total kept while turning, sort and filter as request parameters, and a refused filter or page size shown in the server's words."},
	{"metadata.filterable", "FieldMeta.filterable: the browser learns which columns the server filters from the generated metadata — always for an indexed field and a computed resource's allow_filter field, withIndexed for a table or view allow_filter field, which the server accepts only beside an indexed one — and draws a filter control nowhere else."},
	{"permission-digest", "The permission digest: advisory grant structure the browser gates every request on."},

	// RPC.
	{"rpc.client-form", "Execute(ctx, client resource.Client, rpcClient): a method that runs outside a transaction."},
	{"rpc.typed-result", "Execute's typed second return value, mirrored into TypeScript."},
	{"rpc.nested-shape", "A nested request or row shape mirrored into the handler and the TypeScript client."},
	{"rpc.dry-run", "X-Dry-Run: the frame runs and rolls back; a client-form method refuses with 400."},
	{"rpc.armed-write", "A body's patch armed with Enforce(caller): the caller's own grants decide."},
	{"rpc.armed-read", "A body's query armed with Enforce(caller): the caller's own read grants and masking apply."},
	{"rpc.decision-as-data", "caller.Check: a body branches on a decision without arming a write."},
	{"rpc.as-role", "caller.As over RolePrincipalPermissions: a body acts through a role's checker, the actor's identity kept."},
	{"rpc.trusted-body", "The default: a body's writes run trusted, since the frame's Execute check admitted the caller."},
	{"rpc.row-free", "A method with no @target: its Execute grant stays row-free."},
	{"@answers", "@answers(200, 409) with HTTPStatus(): a method chooses its status; a declared 4xx rolls back and answers the typed body."},
	{"@answers.no-content", "@answers(204): a method that writes no body."},
	{"@upload", "@upload(max): a multipart method whose Execute takes resource.Files."},
	{"rpc.upload-store", "UploadStore: the frame streams files to the application's store, promotes after commit, discards on failure."},

	// Impersonation.
	{"impersonation.view-as", "A session minted as another user under a List, Read mask."},
	{"impersonation.act-as-role", "A session minted as a role: the role's grants, the actor's identity."},
	{"impersonation.mask", "MaskPermissions(fallback, perms...): the session's permission allowlist."},
	{"impersonation.identity-proof", "Under an assumed role, subject binds to the actor: a grant over subject.set is judged against the actor's own attributes."},
	{"impersonation.end", "EndImpersonation: the minted session ends and the actor's own session is restored."},
	{"impersonation.active-list", "ActiveImpersonations: every live impersonated session listed for an operator."},
	{"impersonation.revoke", "DestroyImpersonatedSession: a live impersonated session ended by an operator."},
	{"impersonation.max-duration", "MaxDuration: a hard cap on a minted session's lifetime."},
	{"impersonation.read-only-backstop", "EnforceReadOnlyMask: a write from a read-only session refused before any handler runs."},
	{"impersonation.session-permissions", "SessionPermissions composing the user checker and the role checker from the session's principal."},

	// The application as a whole.
	{"login-manifest", "A login page listing every persona with what its view proves; two clicks to switch."},
	{"walkthrough", "Every persona's proof by curl against a fresh stack."},
	{"bootstrap.target", "The bootstrap picks the emulator or a real instance from the environment alone: the instance is created on the emulator only, the database wherever it is missing, the same steps after."},
	{"bootstrap.reset", "A data-only reset: every table the schema migrations do not populate emptied, children before parents, and the world seeded again, no DDL."},
	{"bootstrap.volume", "Synthetic volume over the seeded world, batched insert mutations in foreign-key order from deterministic identifiers, for reading real query plans."},
	{"authz-matrix", "GenerateHandlerTests: the generated authorization matrix over a fake engine."},
	{"regen-idempotent", "go generate reproduces the generated files byte for byte."},
	{"ci-stub", "The resource module's test stub that runs the application's suite in CI."},
	{"impulse.bootstrapped", "An application born from impulse new and impulse add auth, green under impulse check."},
	{"demonstration-index", "This registry, the Demonstrates declarations, and the generated key-to-files table."},
}

// DemonstrationKeys returns the registry's keys, sorted.
func DemonstrationKeys() []string {
	keys := make([]string, 0, len(Demonstrations))
	for _, d := range Demonstrations {
		keys = append(keys, d.Key)
	}
	sort.Strings(keys)

	return keys
}

// DemonstrationDescriptions returns the registry as a map from key to description.
func DemonstrationDescriptions() map[string]string {
	out := make(map[string]string, len(Demonstrations))
	for _, d := range Demonstrations {
		out[d.Key] = d.Description
	}

	return out
}
