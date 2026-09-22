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
	{"@attribute.nullable-bool", "A nullable BOOL attribute: IS NULL is the only test that admits the undecided row, and = true and = false each exclude it, so 'not refused' is written insured IS NULL OR insured = true; insured != false would drop the undecided outfits."},
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
	{"input_only", "A field accepted on mutations and never serialized back: absent from the TypeScript row interface, writeOnly in its metadata entry, and carried by the Create and Patch shapes."},
	{"output_only", "A field the wire can never write."},
	{"default_create_fn", "A server-side default computed at create time."},
	{"immutable", "A field that can be created but never updated."},
	{"output_only_update_fn", "A mechanical stamp written on every update."},
	{"@defaultsCreateType", "A defaults type run inside the create transaction."},
	{"@validateCreateType", "A validator type run inside the create transaction."},
	{"@defaultsUpdateType", "A defaults type run inside the update transaction."},
	{"@validateUpdateType", "A validator type run inside the update transaction."},
	{"allow_filter", "An unindexed column made filterable."},
	{"nullboolean", "A nullable BOOL column: typed NullBoolean in the generated TypeScript, display type nullboolean in its metadata, rendered by the console as a three-way picker, and written back to NULL by a null in a PATCH body."},
	{"index.tenant-second", "A column directly after the tenant column in an index key is indexed on a bare @domain resource: the list binds the tenant by equality, so a filter on the column alone seeks the index, and the column carries index:\"true\" and filterable: 'always'."},
	{"index.trailing-key", "A trailing key column with nothing bound before it is not indexed: a filter on it alone would scan the index, so the column carries no index tag and no filterable metadata, and a filter naming it alone is refused."},
	{"filter.typed-values", "A filter value typed by its column: a decimal binds as NUMERIC, a non-number is a 400."},
	{"filter.validated-at-decode", "A filter validated with the rest of the request, before permission checks and before any body runs."},
	{"decode.value-limit", "A value the column cannot hold answers 400 naming the field at decode, before anything is buffered and before any permission check: a string over its STRING(n) length by code point, a decimal beyond NUMERIC's 29 integer digits or 9 decimals; the generated sqltype tag carries the column type, and the TypeScript metadata carries maxLength so a form refuses first."},
	{"decode.nullable-slice", "A nullable BYTES or ARRAY column typed by the plain slice: nullability follows the column, a null is accepted at decode where the column allows it and refused with cannot be null where it does not, and the metadata carries required accordingly."},
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
	{"picker.paged", "A picker over a resource that declares a maximum page size (@page max) pages it one server page at a time: Previous and Next inside the panel by the server's cursors, the first page's total, the configured sorts, else the source's @order, else the display column, and the chosen row read by key and shown whichever page is open; the maximum is the switch, read from the generated descriptor, and nothing reads the source whole."},
	{"picker.whole", "A picker over a resource with no maximum page size reads it whole in one request (limit=all) and resolves the chosen row from that list, so a source with no read route serves it; a fixed enumeration renders with no request at all."},
	{"column.referenced-in", "A list column showing a referenced resource's display value resolves it on the referenced resource's read mode: a source with no maximum is read whole once and mapped for every page, a source with a maximum is asked for one filter=id:in:(...) list per page over the page's keys, batched under its maximum and served by the key's index, never read whole."},
	{"@suppress", "@suppress(readHandler | patchHandler): a route not generated."},
	{"@virtual", "A virtual resource over an embedded subquery."},
	{"virtual.with-clause", "A virtual subquery with a WITH clause."},
	{"virtual.named-param", "A virtual subquery with a named parameter."},
	{"virtual.domain", "A tenant-scoped virtual resource over a view column."},
	{"virtual.keyed-read", "A @virtual view that declares its @primarykey serves a keyed read route beside its list, as a table does, so a picker over a bounded view reads the chosen row by key; a view with no key lists only and its metadata says readDisabled."},
	{"@rowsOf", "The struct-scope @rowsOf(Table) on a view: the view declares its backing table, a create goes into the table, and the new row shows up in the view on the next list because the view's SQL reads that table."},
	{"@rowsOf.same-row", "A view keyed by its table's own key: a row opens the table's page, an edit patches the table, and a delete removes the row, while the view's other columns stay its own."},
	{"@rowsOf.association", "Two views over one association table, keyed by the table's compound key under its column names: the key rides in every list request, a row is deleted from the list through the table, and a create associates through the table's form."},
	{"list.keyless", "The library's list page over a key-less resource (a @computed or @virtual struct with no @primarykey, keys: [] in the descriptor) draws the whole list as one page, every row identified by its position: Previous and Next disabled with the count, no View column, no create, no delete, and no row route; a pageSize or enableRowExpansion on its config fails when the page is built naming the resource and the reason, and the grid identifies rows by rowKey, never by a field called id."},
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
	{"computed.null-placement", "A computed list sorts and pages NULL where the application's database does: a pushdown body's plain ORDER BY, its own cursor predicate, and the handler's in-memory sort and boundary test agree across the NULL boundary, whether the body pages itself or the handler pages over its order."},
	{"computed.user", "QuerySet.User(): the identity the check ran as, for a caller-scoped computed read."},
	{"computed.scope", "QuerySet.Scope(): the partition the check ran in."},
	{"computed.keyless", "A @computed struct with no @primarykey is a whole read-only list: one list route serves every row the filter admits in one response, sorted when a sort is asked or declared, with no read route, no page (a numeric limit or a cursor is refused naming @primarykey as the way to page), and no row identity; declaring a key brings all three back."},
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
	{"typescript.derived-object", "A plain struct on a JSON column: the generator derives its TypeScript interface from the struct's json tags into the resource's namespace, types the field object, and writes the Spanner methods that store it, so the application declares the shape once and no client library carries it."},
	{"typescript.imported-type", "@typescript(Name, from: \"module\") on a type whose shape lives outside Go: every generated file that carries the type imports Name from the module, in both clients, and the column is typed Name with display type object."},
	{"typescript.byte-slice", "A []byte column: one leaf to the generator, a string in both clients' interfaces (encoding/json carries it as base64) with display type bytes, never a number[]; the upload method records each document's SHA-256 in it."},
	{"typescript.array-column", "An ARRAY<INT64> column typed []int64: number[] in the generated interface and in the metadata, one member of the display-type vocabulary the client's union lists, so the console builds against what the generator emits; the column takes no filter or index tag, a sort naming it answers 400, and a patch writes the array whole."},
	{"typescript.raw-json", "A computed field typed json.RawMessage, carrying a payload from outside the schema the application does not model: unknown in both clients' interfaces with display type object, as spanner.NullJSON is, passed through verbatim; on a Spanner column the standard-library type is refused until the client stores it as JSON, naming spanner.NullJSON and a declared type over it as the spellings that work."},
	{"typescript.generated-json-methods", "A type declared over json.RawMessage, or over any type with JSON methods, writes no methods of its own: the generator gives back MarshalJSON and UnmarshalJSON converting through the type it is declared over (zz_gen_json.go) beside the Spanner pair, so the type is its declaration and its @typescript annotation and nothing else."},
	{"typescript.types-package", "WithTypes(dir): an application package the generator writes nothing else into, named so a type it declares and a column holds gets its generated JSON and Spanner methods beside it, instead of moving into the resources package or being wrapped."},
	{"auth.password", "A PasswordAuth population whose roles the application owns."},
	{"auth.two-populations", "Two auth packages, each with its own tables, cookies, and permission store, each outlet bound to one."},
	{"auth.directory-roles", "An OIDC auth whose role membership is the directory's (RoleSync), with no role writer in the application."},
	{"auth.skipauth-directory", "The session library's skipAuth build simulating the directory from APP_USERNAME and APP_ROLES."},
	{"auth.login-refusal-code", "A refused login returns to the login page with a code in ?code=, never text: the page holds the sentence for each code and shows nothing for one it does not know."},

	// The browser library's application.
	{"idle.configured", "The idle session configured by the application through the library's six tokens: IDLE_SESSION_DURATION, IDLE_WARNING_DURATION, and IDLE_KEEPALIVE_DURATION read from the build's environment so a development build can shorten them, IDLE_TIMEOUT_REQUIRE_CONFIRMATION true so the warning holds until the stay-logged-in action the idle service exposes is taken, and IDLE_LOGOUT_ACTION and LOGOUT_ACTION as the application's hooks, where the tokens' factory defaults applied."},
	{"config.component", "A componentConfig among a config-driven page's related configs: the application's own component, extending CustomConfigComponent, rendered in the row's page with the row as its parentData, saying what no field renders."},
	{"config.array", "An arrayConfig among a config-driven page's related configs: the rows of a resource its listFilter selects from the page's row, each drawn by the iteratedConfig, read on the listed resource's read mode (whole where it declares no maximum page size, one server page with Previous and Next where it declares one)."},
	{"field.bytes", "A bytes field in the browser library: a grid cell shows the decoded size, never the base64, a view shows the size with a download of the bytes named after the field, and no form mode offers an input, since binary content is written through @upload and served through @file."},
	{"field.array", "An array field in the browser library: a grid cell shows the elements each written by their element type and joined, a view shows them as chips, an empty list is the placeholder, and no form mode offers an input until the array editor lands, so a save of another field never carries the array."},
	{"field.object", "An object field in the browser library, a derived struct, an imported type, spanner.NullJSON, and unknown alike: a grid cell shows the JSON on one line cut with an ellipsis, a view prints it indented, and no form mode offers an input."},
	{"field.write-only", "A write-only field (FieldMeta.writeOnly) in the browser library: absent from a view and a grid cell, a blank input in create and in edit whose value travels only when typed, and a list column naming it fails when the page is built, since a list never returns it; the row's Update envelope names the field when the caller's grant covers it, since the server plans the envelope over every field the caller may write, projected or not, so the edit form draws the input under an envelope too."},
	{"form.leave-page", "The leave-page confirmation: the library's resourceRoutes attaches its canDeactivateGuard to every config-driven list and row route, so leaving a page whose form FormStateService holds dirty opens LeavePageConfirmationModalComponent and a route to FRONTEND_LOGIN_PATH leaves without asking; the application provides the path and adds no guard of its own."},
	{"client.login-redirect", "The client's error hook carries the login redirect: a 401 mid-session, the session gone, keeps the attempted URL (BASE_URL plus the current route) in AuthService.redirectUrl and returns the browser to FRONTEND_LOGIN_PATH, where the login page reads it once and returns there after the login; the application hands the hook to its generated createApi through provideResourceClient, which also counts each request as activity for the progress bar, and keeps no HTTP interceptor."},
	{"client.uncaught-notice", "The notice is for the error nobody caught: an ApiError that reaches the ErrorHandler provideResourceClient installs raises one global notice in the server's words (the body's message, else HTTP <status>), a request that got no response raises one saying so, and a refusal a page reports in place (the flight deck's declared 409 answer and its 403 on an edit, the store's pageError and viewError) raises none, where the interceptor toasted every response beneath the client and doubled every in-place refusal."},

	// Paging.
	{"@order", "@order(Field asc|desc): a list's declared total order."},
	{"@page", "@page(default: N, max: M): a list's page sizes, carried into the descriptor."},
	{"order.none", "A whole list (limit=all) with no @order and no request sort is not sorted: a table statement carries no ORDER BY and its rows arrive in the database's own order, and a computed list keeps the order its body yielded; the same resource paged without a sort is refused."},
	{"order.required", "Every paged list request carries an order: with no @order on the struct and no sort on the request, a bare GET or a limit is refused with a 400 naming the resource and the two ways out (add a sort, or ask limit=all where no maximum is declared), so a page never hides that more rows exist without saying where they are."},
	{"paging.cursor", "A keyset cursor: pages positioned by the row the last page ended on."},
	{"paging.link-header", "The Link header's next and prev relations, followed exactly as issued."},
	{"paging.total-count", "count=true on a first page answers Total-Count."},
	{"paging.limit-all", "limit=all, legal only where no maximum is declared."},
	{"paging.offset-refused", "The offset parameter is refused."},
	{"paging.readability-rule", "A sort or filter on a field the caller is denied is refused naming the field."},
	{"paging.masked-sort", "A sort or filter on a conditionally granted field runs over the visible projection: masked cells are NULL."},
	{"paging.unselected-sort-key", "A sort key outside the columns projection, the primary key included, pages on the cursor's copy of the value: the statement selects it a second time under a reserved column, and the row data carries nothing extra."},
	{"paging.named-variant-key", "A sort key of a named variant type (type HazardLevel int64), concealing and conditionally granted, left out of columns=: the cursor's copy decodes into the field's own type, and masked cells walk through the NULL region."},
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
	{"rpc.upload-store", "FileStore: the frame streams files to the application's store under minted keys, the transaction's commit claims them, and a failure before commit deletes them; an object no row claims is the application's sweep's."},

	// Files.
	{"@file.stored", "@file on the column holding a stored file's key: the generator serves the file under the row's read route, gated by Read on the resource and a Read grant on the route's own field (content), typed and named from the row's columns, the key its validator, and the key column off the wire in both directions; NOT NULL, the key leaves the resource no Create."},
	{"@file.rendered", "The struct-scope @file on a keyed @computed struct: the computed package's <Name><Segment> function renders the document at request time as a resource.Content the generated route serves under the same gate, a nil content 404, the content's tag its validator."},
	{"@file.released", "Deleting a row that carries a @file key releases its object: the patch machinery records the key on the transaction and the executor deletes it from the client's FileStore (WithFileStore) once the commit lands; a transaction that does not commit releases nothing."},
	{"@file.replaced", "Pointing a @file row at another object releases the old one: an @upload method's body sets the key column and the executor deletes the object the row held after the commit; a dry run streams nothing and releases nothing."},

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
