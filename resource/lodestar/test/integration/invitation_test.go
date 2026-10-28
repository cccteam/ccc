//go:build skipAuth

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc"
	"github.com/cccteam/session/mail"
)

// This suite drives the ways a person comes to hold an account and a seat without an
// operator typing their name, on the served stack, following each mailed link the way
// the person does: the recorder the two auths send through holds every message, and the
// suite reads the token from the link in it.
//
// The crew's routes: the global partition's invitations under the console's API, the
// sector partition's under the sector's URL, the session's invitation, email and sign-up
// routes under the console's user prefix.

const (
	// A strong enough password for the sign-ups.
	signUpPassword = "orbit-and-anchor"
)

// crewGlobalRoute is the console's route of one of the crew's global-partition resources.
func crewGlobalRoute(route string) string {
	return consoleAPI + "/" + route
}

// lastMailTo answers the last message the auths sent to the address and the token in
// its link under the query parameter named (invitation, verify, signUp).
func lastMailTo(t *testing.T, s *served, to, param string) (message mail.Message, token string) {
	t.Helper()

	messages := s.mail.Messages()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].To != to {
			continue
		}
		for _, word := range strings.Fields(messages[i].Text) {
			link, err := url.Parse(word)
			if err != nil || link.Scheme == "" {
				continue
			}
			if token := link.Query().Get(param); token != "" {
				return messages[i], token
			}
		}
		t.Fatalf("the message to %s carries no link with ?%s=: %q", to, param, messages[i].Text)
	}
	t.Fatalf("no message was sent to %s", to)

	return mail.Message{}, ""
}

// inviteBody is the operations body that invites the address with the roles.
func inviteBody(email string, roles ...string) []byte {
	quoted := make([]string, 0, len(roles))
	for _, role := range roles {
		quoted = append(quoted, fmt.Sprintf("%q", role))
	}

	return []byte(fmt.Sprintf(`[{"op":"add","path":"/","value":{"email":%q,"roles":[%s]}}]`, email, strings.Join(quoted, ",")))
}

// invitations reads the standing invitations at the route as the browser.
func invitations(ctx context.Context, t *testing.T, b *browser, route string) []map[string]any {
	t.Helper()

	status, body := b.do(ctx, http.MethodGet, route, nil)
	if status != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", route, status, body)
	}
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decoding the invitations: %v: %s", err, body)
	}

	return rows
}

// userRow finds the user by name among the users at the route, or nil.
func userRow(ctx context.Context, t *testing.T, b *browser, route, username string) map[string]any {
	t.Helper()

	status, body := b.do(ctx, http.MethodGet, route, nil)
	if status != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", route, status, body)
	}
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decoding the users: %v: %s", err, body)
	}
	for _, row := range rows {
		if row["username"] == username {
			return row
		}
	}

	return nil
}

// rolesOf reads the roles a user row carries, sorted.
func rolesOf(row map[string]any) []string {
	var roles []string
	if list, ok := row["roles"].([]any); ok {
		for _, role := range list {
			if name, ok := role.(string); ok {
				roles = append(roles, name)
			}
		}
	}
	slices.Sort(roles)

	return roles
}

// idOf reads a row's id.
func idOf(t *testing.T, row map[string]any) string {
	t.Helper()

	id, ok := row["id"].(string)
	if !ok {
		t.Fatalf("the row carries no id: %v", row)
	}

	return id
}

// TestInvitation_globalSignUpWithPassword invites a stranger into the global partition
// and follows the link to a password login: the governor (Headquarters, which holds
// Create on CrewInvitations) invites an address with a global role, the recorder holds
// the message, the stranger opens the invitation on a fresh browser, creates a login
// with a password, and is signed in holding the role; the invitation is consumed, the
// address is theirs verified, and when the governor removes that one seat, the last the
// person holds anywhere, the account goes with it.
//
// Demonstrates: auth.invitation, auth.email-address.
func TestInvitation_globalSignUpWithPassword(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	names := crewPartitions().Global
	invitationsRoute := crewGlobalRoute(names.InvitationsRoute)
	usersRoute := crewGlobalRoute(names.UsersRoute)
	seatsRoute := crewGlobalRoute(names.SeatsRoute)
	const email, username = "ensign@example.com", "ensign"

	governor := newBrowser(t, s, consoleAPI)
	governor.signIn(ctx, "governor")
	if status, body := governor.do(ctx, http.MethodPatch, invitationsRoute, inviteBody(email, "ClientBrowser")); status != http.StatusOK {
		t.Fatalf("the governor's invitation: status %d: %s", status, body)
	}
	standing := invitations(ctx, t, governor, invitationsRoute)
	if len(standing) != 1 || standing[0]["email"] != email {
		t.Fatalf("standing invitations = %v, want the one to %s", standing, email)
	}
	message, token := lastMailTo(t, s, email, "invitation")
	if !strings.Contains(message.Text, "ClientBrowser") && !strings.Contains(message.Subject, "Lodestar") {
		t.Errorf("the invitation names neither the application nor the role: %q / %q", message.Subject, message.Text)
	}

	// The stranger opens the link on a browser of their own: the page posts the token,
	// learns the address and that a password login may be created, and creates one.
	stranger := newBrowser(t, s, consoleAPI)
	if status, body := stranger.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before the invitation: status %d: %s", status, body)
	}
	status, body := stranger.do(ctx, http.MethodPost, consoleAPI+"/user/invitation", jsonBody(t, map[string]string{"token": token}))
	if status != http.StatusOK {
		t.Fatalf("POST invitation: status %d: %s", status, body)
	}
	var opened struct {
		Email  string `json:"email"`
		SignUp bool   `json:"signUp"`
	}
	if err := json.Unmarshal(body, &opened); err != nil || opened.Email != email || !opened.SignUp {
		t.Fatalf("the opened invitation = %s (%v), want the address %s with sign-up offered", body, err, email)
	}
	if status, body := stranger.do(ctx, http.MethodPost, consoleAPI+"/user/invitation/sign-up", jsonBody(t, map[string]string{"username": username, "password": signUpPassword})); status != http.StatusOK {
		t.Fatalf("POST invitation/sign-up: status %d: %s", status, body)
	}
	// The login is created and signed in, and the sign-in waits for the second factor
	// as every session does: the code goes to the invitation's address, now theirs.
	stranger.secondFactor(ctx, username, false)
	sess := stranger.session(ctx)
	if sess["authenticated"] != true || sess["username"] != username || sess["email"] != email || sess["emailVerified"] != true {
		t.Fatalf("session after the sign-up = %v, want %s signed in with %s verified", sess, username, email)
	}

	// Seated by the acceptance: the governor's list shows the role, and the invitation
	// is gone.
	row := userRow(ctx, t, governor, usersRoute, username)
	if row == nil || !equalStrings(rolesOf(row), []string{"ClientBrowser"}) || row["email"] != email || row["emailVerifiedAt"] == nil {
		t.Fatalf("the ensign's row = %v, want ClientBrowser with the verified address", row)
	}
	if standing := invitations(ctx, t, governor, invitationsRoute); len(standing) != 0 {
		t.Errorf("standing invitations after the acceptance = %v, want none", standing)
	}

	// The orphan rule: the governor removes the one seat, and the account goes with it.
	id, err := ccc.UUIDFromString(idOf(t, row))
	if err != nil {
		t.Fatal(err)
	}
	if status, body := governor.do(ctx, http.MethodPatch, seatsRoute, []byte(unseatBody(id, "ClientBrowser"))); status != http.StatusOK {
		t.Fatalf("the governor unseats the ensign: status %d: %s", status, body)
	}
	if row := userRow(ctx, t, governor, usersRoute, username); row != nil {
		t.Errorf("the ensign's row after the last seat went = %v, want the account deleted", row)
	}
	if sess := stranger.session(ctx); sess["authenticated"] == true {
		t.Errorf("the ensign's session after the account went = %v, want ended", sess)
	}
}

// TestInvitation_sectorAcceptedBySignedInPersona invites an address into Anvil and has
// a persona who already holds a login accept it: the marshal (SectorMarshal at Anvil,
// Create on CrewSectorInvitations in every role but its own) invites the cadet's
// address as a harbormaster, cannot invite a marshal, and the cadet, signed in with her
// password, opens the link and accepts: she holds the seat, and the address, which she
// held none of, is hers verified. A revoked invitation is one the link no longer opens.
//
// Demonstrates: auth.invitation.
func TestInvitation_sectorAcceptedBySignedInPersona(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	names := crewPartitions().Tenant
	invitationsRoute := sectorPath(anvil, names.InvitationsRoute)
	const email = "cass@example.com"

	marshal := newBrowser(t, s, consoleAPI)
	marshal.signIn(ctx, "marshal")
	if status, body := marshal.do(ctx, http.MethodPatch, invitationsRoute, inviteBody(email, string(marshalRole))); status != http.StatusForbidden {
		t.Fatalf("the marshal invites a marshal: status %d, want 403: %s", status, body)
	}
	if status, body := marshal.do(ctx, http.MethodPatch, sectorPath(bastion, names.InvitationsRoute), inviteBody(email, string(harbormasterRole))); status != http.StatusNotFound {
		t.Fatalf("the marshal invites into Bastion: status %d, want 404: %s", status, body)
	}
	if status, body := marshal.do(ctx, http.MethodPatch, invitationsRoute, inviteBody(email, string(harbormasterRole))); status != http.StatusOK {
		t.Fatalf("the marshal's invitation: status %d: %s", status, body)
	}
	_, token := lastMailTo(t, s, email, "invitation")

	// A second invitation, revoked before anyone follows it.
	if status, body := marshal.do(ctx, http.MethodPatch, invitationsRoute, inviteBody("later@example.com", string(harbormasterRole))); status != http.StatusOK {
		t.Fatalf("the marshal's second invitation: status %d: %s", status, body)
	}
	_, revokedToken := lastMailTo(t, s, "later@example.com", "invitation")
	var revokedID string
	for _, inv := range invitations(ctx, t, marshal, invitationsRoute) {
		if inv["email"] == "later@example.com" {
			revokedID = idOf(t, inv)
		}
	}
	if status, body := marshal.do(ctx, http.MethodPatch, invitationsRoute, []byte(fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, revokedID))); status != http.StatusOK {
		t.Fatalf("the marshal revokes: status %d: %s", status, body)
	}

	cadet := newBrowser(t, s, consoleAPI)
	cadet.signIn(ctx, string(policyCadet))
	if status, body := cadet.do(ctx, http.MethodPost, consoleAPI+"/user/invitation", jsonBody(t, map[string]string{"token": revokedToken})); status == http.StatusOK {
		t.Fatalf("the revoked invitation opens: status %d: %s", status, body)
	}
	if status, body := cadet.do(ctx, http.MethodPost, consoleAPI+"/user/invitation", jsonBody(t, map[string]string{"token": token})); status != http.StatusOK {
		t.Fatalf("the cadet opens the invitation: status %d: %s", status, body)
	}
	if status, body := cadet.do(ctx, http.MethodPost, consoleAPI+"/user/invitation/accept", nil); status != http.StatusOK {
		t.Fatalf("the cadet accepts: status %d: %s", status, body)
	}
	// The address she gave for her sign-in code stays hers: an invitation's address
	// lands on a person who holds none (the ensign's sign-up), never over one held.
	sess := cadet.session(ctx)
	if sess["email"] != factorAddress(string(policyCadet)) || sess["emailVerified"] != true {
		t.Errorf("the cadet's session after the acceptance = %v, want the address she holds, %s, verified", sess, factorAddress(string(policyCadet)))
	}
	row := userRow(ctx, t, marshal, crewSectorUsers(anvil), string(policyCadet))
	if row == nil || !equalStrings(rolesOf(row), []string{"Cadet", string(harbormasterRole), "SectorCrew"}) {
		t.Fatalf("the cadet's row at Anvil = %v, want Cadet, Harbormaster and SectorCrew", row)
	}
	if standing := invitations(ctx, t, marshal, invitationsRoute); len(standing) != 0 {
		t.Errorf("Anvil's standing invitations = %v, want none", standing)
	}
}

// TestEmailAddress_addedAndVerified has a persona change her address and verify the new
// one from the mailed link: the pilot holds the address she gave for her sign-in code,
// the add mails the link and writes nothing on the user, who holds the old address
// until the token is posted back, when the new one lands verified; the address is then
// taken, so another persona who gives it is mailed a notice instead of a link and keeps
// her own.
//
// Demonstrates: auth.email-address.
func TestEmailAddress_addedAndVerified(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	const email = "pax@example.com"

	pilot := newBrowser(t, s, consoleAPI)
	pilot.signIn(ctx, "pilot")
	if sess := pilot.session(ctx); sess["email"] != factorAddress("pilot") || sess["emailVerified"] != true {
		t.Fatalf("the pilot's session before the add = %v, want the address she gave for her sign-in code, verified", sess)
	}
	if status, body := pilot.do(ctx, http.MethodPost, consoleAPI+"/user/email", jsonBody(t, map[string]string{"email": email})); status != http.StatusOK {
		t.Fatalf("POST email: status %d: %s", status, body)
	}
	if sess := pilot.session(ctx); sess["email"] != factorAddress("pilot") {
		t.Fatalf("the pilot's session after the add = %v, want the old address until the link is followed", sess)
	}
	_, token := lastMailTo(t, s, email, "verify")
	if status, body := pilot.do(ctx, http.MethodPost, consoleAPI+"/user/email/verify", jsonBody(t, map[string]string{"token": token})); status != http.StatusOK {
		t.Fatalf("POST email/verify: status %d: %s", status, body)
	}
	if sess := pilot.session(ctx); sess["email"] != email || sess["emailVerified"] != true {
		t.Fatalf("the pilot's session after the verification = %v, want %s verified", sess, email)
	}

	// The address is taken: another persona who gives it is mailed a notice, not a
	// link, and the screen says nothing either way.
	veteran := newBrowser(t, s, consoleAPI)
	veteran.signIn(ctx, "veteran")
	sent := len(s.mail.Messages())
	if status, body := veteran.do(ctx, http.MethodPost, consoleAPI+"/user/email", jsonBody(t, map[string]string{"email": email})); status != http.StatusOK {
		t.Fatalf("the veteran gives the pilot's address: status %d: %s", status, body)
	}
	messages := s.mail.Messages()
	if len(messages) != sent+1 || messages[sent].To != email || strings.Contains(messages[sent].Text, "verify=") {
		t.Errorf("the mail for a held address = %+v, want one notice to %s without a verification link", messages[sent:], email)
	}
	if sess := veteran.session(ctx); sess["email"] != factorAddress("veteran") {
		t.Errorf("the veteran's session = %v, want her own address kept", sess)
	}
}

// TestPasswordSignUp_fromMailedLink creates a crew login with a password from a sign-up
// link: a stranger gives an address, follows the link, opens it for the address, and
// completes with a username and a password; the login holds the address verified and
// no role, and an address an account already holds answers the same to the screen.
//
// Demonstrates: auth.password-sign-up, auth.email-address.
func TestPasswordSignUp_fromMailedLink(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	const email, username = "rookie@example.com", "rookie"

	stranger := newBrowser(t, s, consoleAPI)
	if status, body := stranger.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before the sign-up: status %d: %s", status, body)
	}
	if status, body := stranger.do(ctx, http.MethodPost, consoleAPI+"/user/sign-up", jsonBody(t, map[string]string{"email": email})); status != http.StatusOK {
		t.Fatalf("POST sign-up: status %d: %s", status, body)
	}
	_, token := lastMailTo(t, s, email, "signUp")
	status, body := stranger.do(ctx, http.MethodPost, consoleAPI+"/user/sign-up/open", jsonBody(t, map[string]string{"token": token}))
	if status != http.StatusOK {
		t.Fatalf("POST sign-up/open: status %d: %s", status, body)
	}
	var opened struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &opened); err != nil || opened.Email != email {
		t.Fatalf("the opened sign-up = %s (%v), want the address %s", body, err, email)
	}
	if status, body := stranger.do(ctx, http.MethodPost, consoleAPI+"/user/sign-up/complete", jsonBody(t, map[string]string{"token": token, "username": username, "password": signUpPassword})); status != http.StatusOK {
		t.Fatalf("POST sign-up/complete: status %d: %s", status, body)
	}
	// Signed in, and waiting for the second factor as every session does: the code
	// goes to the address the sign-up verified.
	stranger.secondFactor(ctx, username, false)
	sess := stranger.session(ctx)
	if sess["authenticated"] != true || sess["username"] != username || sess["email"] != email || sess["emailVerified"] != true {
		t.Fatalf("session after the sign-up = %v, want %s signed in with %s verified", sess, username, email)
	}
	if got := stranger.domains(ctx); len(got) != 0 {
		t.Errorf("user-domains after the sign-up = %v, want none: the login holds no role", got)
	}

	// The same address again answers the same to the screen, and the mail points at
	// the account instead of carrying a sign-up link.
	sent := len(s.mail.Messages())
	another := newBrowser(t, s, consoleAPI)
	if status, body := another.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session: status %d: %s", status, body)
	}
	if status, body := another.do(ctx, http.MethodPost, consoleAPI+"/user/sign-up", jsonBody(t, map[string]string{"email": email})); status != http.StatusOK {
		t.Fatalf("POST sign-up with a held address: status %d: %s", status, body)
	}
	messages := s.mail.Messages()
	if len(messages) != sent+1 || messages[sent].To != email || strings.Contains(messages[sent].Text, "signUp=") {
		t.Errorf("the mail for a held address = %+v, want one message to %s without a sign-up link", messages[sent:], email)
	}
}

// jsonBody encodes a request body.
func jsonBody(t *testing.T, v any) []byte {
	t.Helper()

	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return body
}
