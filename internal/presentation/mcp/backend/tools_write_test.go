// Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>
//
// WebSocket API Access Policy:
// This WebSocket API is reserved exclusively for Saubeo Solutions and its native applications.
// Unauthorized access is strictly prohibited.
// See API_ACCESS_POLICY.md for full terms.

package backend

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"rdio-scanner/internal/domain"
)

// The administrator's write capabilities. Each one changes the deployment, so each one says what it
// is about to change and asks the operator before it changes it: the assistant states the change,
// the operator confirms it, and the write happens only in the confirmed round.
//
// These are the tests for that contract. A refused ask changes nothing at all. An accepted ask
// changes exactly what was named, and nothing beside it. A change that removes entries names them,
// because a section replace deletes rows the operator cannot get back. A client that cannot ask the
// operator cannot make a change. And every one of these capabilities declares, in the catalog, that
// it is not a read.

// confirmer is the operator standing at the client: what the surface asked, and what the operator
// answered at each ask.
type confirmer struct {
	action string
	asks   []string
}

func (c *confirmer) handler() func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
	return func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		c.asks = append(c.asks, req.Params.Message)

		if c.action != "accept" {
			return &mcp.ElicitResult{Action: c.action}, nil
		}

		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirmed": true}}, nil
	}
}

// asked is everything the surface said at its asks, as one block of text.
func (c *confirmer) asked() string {
	return strings.Join(c.asks, "\n")
}

// newConfirmingSession is the administrator's capability set with a client that answers the surface's
// asks. The action is what the operator does at the prompt: accept, decline, or dismiss it.
func newConfirmingSession(t *testing.T, action string) (*session, *confirmer) {
	t.Helper()

	operator := &confirmer{action: action}

	bundle := newServer(t, true)

	s := newSessionOn(t, bundle, bundle.server.full, Caller{Tier: TierFull}, &mcp.ClientOptions{
		ElicitationHandler: operator.handler(),
	})

	return s, operator
}

// deploymentState is the whole configuration the fixture's stores hold, as one comparable string.
// A test that says a refused ask changed nothing has to be able to prove it about every store the
// surface can write to, and not about the one it happened to be aiming at.
func deploymentState(t *testing.T, bundle *serverBundle) string {
	t.Helper()

	body, err := json.Marshal(struct {
		Accesses    domain.Accesses
		Apikeys     domain.Apikeys
		Dirwatches  domain.Dirwatches
		Downstreams domain.Downstreams
		Systems     domain.Systems
		Groups      domain.Groups
		Tags        domain.Tags
		Options     domain.Options
	}{
		Accesses:    bundle.accesses.Stored,
		Apikeys:     bundle.apikeys.Stored,
		Dirwatches:  bundle.dirwatches.Stored,
		Downstreams: bundle.downstreams.Stored,
		Systems:     bundle.systems.Stored,
		Groups:      bundle.groups.Stored,
		Tags:        bundle.tags.Stored,
		Options:     bundle.options.Stored,
	})
	if err != nil {
		t.Fatalf("the deployment state does not encode: %v", err)
	}

	return string(body)
}

// writeProbes is each write capability with a change it can be asked to make against the fixture:
// the arguments, and the store the change lands in.
func writeProbes() []struct {
	tool string
	args map[string]any
	what string
} {
	return []struct {
		tool string
		args map[string]any
		what string
	}{
		{
			tool: "manage_config",
			args: map[string]any{
				"section": "talkgroups",
				"system":  float64(systemOneRef),
				"payload": []any{
					map[string]any{"id": float64(1), "talkgroupRef": float64(1514000), "label": "Talkgroup", "tagId": float64(3)},
					map[string]any{"id": float64(2), "talkgroupRef": float64(1514001), "label": "Second Talkgroup", "tagId": float64(3)},
					map[string]any{"id": float64(0), "talkgroupRef": float64(1234), "label": "Fire Dispatch", "tagId": float64(3)},
				},
			},
			what: "a talkgroup added to the first system",
		},
		{
			tool: "manage_access_user",
			args: map[string]any{
				"action":     "add",
				"ident":      "volunteer",
				"systems":    []any{map[string]any{"systemRef": float64(systemOneRef), "talkgroups": []any{float64(1514000)}}},
				"expiration": "2026-12-31T00:00:00Z",
			},
			what: "a listener access code",
		},
		{
			tool: "rotate_admin_password",
			args: map[string]any{"newPassword": "a-long-new-administrator-credential"},
			what: "the administrator credential",
		},
	}
}

// A refused confirmation changes nothing. Not the section the change aimed at, and not anything
// beside it: the write never started.
func TestARefusedConfirmationChangesNothing(t *testing.T) {
	for _, probe := range writeProbes() {
		for _, action := range []string{"decline", "cancel"} {
			s, operator := newConfirmingSession(t, action)

			before := deploymentState(t, s.bundle)

			refused := refusedCall(t, s, probe.tool, probe.args)

			if len(operator.asks) != 1 {
				t.Fatalf("%s asked %d times for a %s, want one ask", probe.tool, len(operator.asks), action)
			}

			if !strings.Contains(strings.ToLower(operator.asked()), strings.ToLower(probe.what)) &&
				!strings.Contains(strings.ToLower(operator.asked()), strings.ToLower(probe.tool)) {
				t.Fatalf("%s asked %q, which does not say what it is about to change (%s)",
					probe.tool, operator.asked(), probe.what)
			}

			if after := deploymentState(t, s.bundle); after != before {
				t.Fatalf("%s with a %s changed the deployment:\n%s\n%s", probe.tool, action, before, after)
			}

			if refused == "" {
				t.Fatalf("%s with a %s answered as if the change had been made", probe.tool, action)
			}
		}
	}
}

// An accepted confirmation makes exactly the change that was named, and nothing beside it.
func TestAnAcceptedConfirmationMakesExactlyTheStatedChange(t *testing.T) {
	t.Run("a talkgroup added to one system leaves every other system alone", func(t *testing.T) {
		s, _ := newConfirmingSession(t, "accept")

		call(t, s, "manage_config", map[string]any{
			"section": "talkgroups",
			"system":  float64(systemOneRef),
			"payload": []any{
				map[string]any{"id": float64(1), "talkgroupRef": float64(1514000), "label": "Talkgroup", "tagId": float64(3)},
				map[string]any{"id": float64(2), "talkgroupRef": float64(1514001), "label": "Second Talkgroup", "tagId": float64(3)},
				map[string]any{"id": float64(0), "talkgroupRef": float64(1234), "label": "Fire Dispatch", "tagId": float64(3)},
			},
		})

		first, _ := s.bundle.systems.Stored.ByRef(systemOneRef)
		second, _ := s.bundle.systems.Stored.ByRef(systemTwoRef)

		if len(first.Talkgroups) != 3 {
			t.Fatalf("the first system holds %d talkgroups, want the two it had and the one added: %+v",
				len(first.Talkgroups), first.Talkgroups)
		}
		if len(second.Talkgroups) != 1 {
			t.Fatalf("the second system's talkgroups changed with the first system's: %+v", second.Talkgroups)
		}

		var added *domain.Talkgroup

		for i, talkgroup := range first.Talkgroups {
			if talkgroup.TalkgroupRef == 1234 {
				added = &first.Talkgroups[i]
			}
		}

		if added == nil {
			t.Fatalf("the talkgroup the change named is not on the system: %+v", first.Talkgroups)
		}
		if added.Label != "Fire Dispatch" {
			t.Fatalf("the talkgroup was stored as %q, want the label that was named: %+v", added.Label, added)
		}
	})

	t.Run("the settings that were not named keep the values they had", func(t *testing.T) {
		s, _ := newConfirmingSession(t, "accept")

		call(t, s, "manage_config", map[string]any{
			"section": "options",
			"payload": map[string]any{
				"audioConversion":           float64(domain.AudioConversionEnabled),
				"autoPopulate":              true,
				"branding":                  "",
				"disableDuplicateDetection": false,
				"email":                     "",
				"keypadBeeps":               "uniden",
				"maxClients":                float64(50),
				"pruneDays":                 float64(7),
				"showListenersCount":        false,
				"sortTalkgroups":            false,
			},
		})

		options := s.bundle.options.Stored

		if options.MaxClients != 50 {
			t.Fatalf("the listener ceiling is %d, want the value that was named", options.MaxClients)
		}
		if options.PruneDays != 7 {
			t.Fatalf("the retention horizon moved to %d, want the value that was not named: %+v",
				options.PruneDays, options)
		}
		if !options.AutoPopulate {
			t.Fatalf("auto-populate moved to %v, want the value that was not named: %+v",
				options.AutoPopulate, options)
		}
	})

	t.Run("a listener code is added with the identity and the scope that were named", func(t *testing.T) {
		s, _ := newConfirmingSession(t, "accept")

		call(t, s, "manage_access_user", map[string]any{
			"action":     "add",
			"ident":      "volunteer",
			"systems":    []any{map[string]any{"systemRef": float64(systemOneRef), "talkgroups": []any{float64(1514000)}}},
			"expiration": "2026-12-31T00:00:00Z",
		})

		var added *domain.Access

		for i, access := range s.bundle.accesses.Stored {
			if access.Ident == "volunteer" {
				added = &s.bundle.accesses.Stored[i]
			}
		}

		if added == nil {
			t.Fatalf("no access code carries the identity that was named: %+v", s.bundle.accesses.Stored)
		}
		if len(s.bundle.accesses.Stored) != 2 {
			t.Fatalf("the access list holds %d codes, want the fixture's and the new one: %+v",
				len(s.bundle.accesses.Stored), s.bundle.accesses.Stored)
		}
		if added.Expiration == 0 {
			t.Fatalf("the code was stored with no expiry, want the moment that was named: %+v", added)
		}
		if !added.Scope.GrantsReferences(systemOneRef, 1514000) {
			t.Fatalf("the code does not grant the talkgroup that was named: %+v", added.Scope)
		}
		if added.Scope.GrantsReferences(systemTwoRef, 1514002) {
			t.Fatalf("the code grants a system that was not named: %+v", added.Scope)
		}
	})

	t.Run("the administrator credential is replaced and the server stops asking for it", func(t *testing.T) {
		s, _ := newConfirmingSession(t, "accept")

		call(t, s, "rotate_admin_password", map[string]any{"newPassword": "a-long-new-administrator-credential"})

		if s.bundle.options.Stored.AdminPasswordHash == "" {
			t.Fatalf("the deployment still holds no administrator credential: %+v", s.bundle.options.Stored)
		}
		if s.bundle.options.Stored.AdminPasswordNeedChange {
			t.Fatalf("the server still says the credential has to be replaced: %+v", s.bundle.options.Stored)
		}
	})
}

// The ask names every entry that will change, by the reference an operator reads it by. An ask that
// says "3 changes" is an ask the operator cannot read: the point of confirming is knowing what is
// being confirmed.
func TestTheConfirmationNamesEveryEntryThatWillChange(t *testing.T) {
	s, operator := newConfirmingSession(t, "decline")

	refusedCall(t, s, "manage_config", map[string]any{
		"section": "talkgroups",
		"system":  float64(systemOneRef),
		"payload": []any{
			map[string]any{"id": float64(1), "talkgroupRef": float64(1514000), "label": "Renamed Talkgroup", "tagId": float64(3)},
			map[string]any{"id": float64(0), "talkgroupRef": float64(1234), "label": "Fire Dispatch", "tagId": float64(3)},
		},
	})

	asked := operator.asked()

	for _, want := range []string{"1514000", "1514001", "1234"} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the ask says %q, which does not name talkgroup %s", asked, want)
		}
	}
}

// A section replace deletes the rows the submission does not carry, and the ask says so before it
// asks. An operator confirming "replace the talkgroups of system 1" has to know that the talkgroup
// not in the payload is gone.
func TestASectionReplaceThatRemovesEntriesNamesThemBeforeItAsks(t *testing.T) {
	s, operator := newConfirmingSession(t, "decline")

	refusedCall(t, s, "manage_config", map[string]any{
		"section": "talkgroups",
		"system":  float64(systemOneRef),
		"payload": []any{
			map[string]any{"id": float64(1), "talkgroupRef": float64(1514000), "label": "Talkgroup", "tagId": float64(3)},
		},
	})

	asked := strings.ToLower(operator.asked())

	if !strings.Contains(asked, "1514001") {
		t.Fatalf("the ask says %q, which does not name the talkgroup that will go", operator.asked())
	}

	saidIt := false

	for _, want := range []string{"remov", "delet", "gone"} {
		if strings.Contains(asked, want) {
			saidIt = true

			break
		}
	}

	if !saidIt {
		t.Fatalf("the ask says %q, which does not say that entries will be removed", operator.asked())
	}
}

// A submission that names the section exactly as it is changes nothing: the capability asks nothing,
// writes nothing, and says there is nothing to confirm. An operator confirming a no-op learns
// nothing, and an assistant that asks for one spends the operator's attention for nothing.
func TestAChangeThatChangesNothingAsksNothingAndWritesNothing(t *testing.T) {
	s, operator := newConfirmingSession(t, "accept")

	before := deploymentState(t, s.bundle)

	answered := call(t, s, "manage_config", map[string]any{
		"section": "talkgroups",
		"system":  float64(systemOneRef),
		"payload": []any{
			map[string]any{"id": float64(1), "talkgroupRef": float64(1514000), "label": "Talkgroup", "tagId": float64(3)},
			map[string]any{"id": float64(2), "talkgroupRef": float64(1514001), "label": "Second Talkgroup", "tagId": float64(3)},
		},
	})

	if len(operator.asks) != 0 {
		t.Fatalf("a change that changes nothing asked %d times: %s", len(operator.asks), operator.asked())
	}

	if after := deploymentState(t, s.bundle); after != before {
		t.Fatalf("a change that changes nothing wrote to the deployment:\n%s\n%s", before, after)
	}

	var answer struct {
		Outcome string `json:"outcome"`
	}
	decode(t, answered, &answer)

	if !strings.Contains(strings.ToLower(answer.Outcome), "nothing") {
		t.Fatalf("the answer is %q, which does not say there was nothing to change: %+v", answer.Outcome, answered.StructuredContent)
	}
}

// Every write capability declares, in the catalog an assistant reads, that it is not a read and that
// it can destroy data. An assistant that plans a sequence of calls reads the annotations first: a
// capability that quietly looks like a read is a capability that gets called while the operator is
// looking away.
func TestTheWriteCapabilitiesDeclareTheyAreNotReadOnlyAndAreDestructive(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	listed, err := s.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list the tools: %v", err)
	}

	for _, tool := range listed.Tools {
		capability, ok := catalogOf(tool.Name)
		if !ok {
			continue
		}

		if capability.Effect != EffectWrite && capability.Effect != EffectDestructive {
			continue
		}

		if tool.Annotations == nil {
			t.Fatalf("%s carries no annotations, so an assistant cannot tell what calling it does", tool.Name)
		}

		if tool.Annotations.ReadOnlyHint {
			t.Fatalf("%s declares itself readable: %+v", tool.Name, tool.Annotations)
		}

		if tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
			t.Fatalf("%s declares itself non-destructive: %+v", tool.Name, tool.Annotations)
		}
	}
}

// A client that cannot ask the operator cannot make a change. The confirmation is not a courtesy
// the surface performs when it is convenient: it is the operator in the loop, and a client that has
// no way to reach the operator has no way to bring the operator into the loop.
func TestAWriteRefusesAClientThatCannotAskTheOperator(t *testing.T) {
	for _, probe := range writeProbes() {
		s := newCallerSession(t, Caller{Tier: TierFull})

		before := deploymentState(t, s.bundle)

		refused := refusedCall(t, s, probe.tool, probe.args)

		if !strings.Contains(strings.ToLower(refused), "confirm") {
			t.Fatalf("%s refused with %q, which does not say the change needs the operator's confirmation",
				probe.tool, refused)
		}

		if after := deploymentState(t, s.bundle); after != before {
			t.Fatalf("%s wrote to a deployment whose client cannot ask: \n%s\n%s", probe.tool, before, after)
		}
	}
}

// Removing a listener by identity deletes every access code sharing that name. The ask names every
// one of them, because the operator is confirming a deletion whose size the operator cannot see
// from the name alone.
func TestTheRemoveFormNamesEveryCodeThatWillGo(t *testing.T) {
	s, operator := newConfirmingSession(t, "decline")

	s.bundle.accesses.Stored = append(s.bundle.accesses.Stored,
		domain.Access{ID: 2, Code: "111111", Ident: "volunteer"},
		domain.Access{ID: 3, Code: "222222", Ident: "volunteer"},
	)

	before := deploymentState(t, s.bundle)

	refusedCall(t, s, "manage_access_user", map[string]any{"action": "remove", "ident": "volunteer"})

	asked := operator.asked()

	for _, want := range []string{"2", "3"} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the ask says %q, which does not name the access entry %s that will go", asked, want)
		}
	}

	if after := deploymentState(t, s.bundle); after != before {
		t.Fatalf("a refused removal changed the access list:\n%s\n%s", before, after)
	}
}

// The credential change invalidates the sessions minted with the old credential. The ask says so,
// because the operator is the one who has to re-establish them.
func TestThePasswordChangeNamesWhatItInvalidates(t *testing.T) {
	s, operator := newConfirmingSession(t, "decline")

	refusedCall(t, s, "rotate_admin_password", map[string]any{"newPassword": "a-long-new-administrator-credential"})

	asked := strings.ToLower(operator.asked())

	for _, want := range []string{"session", "sign"} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the ask says %q, which does not say what the credential change invalidates (%s)",
				operator.asked(), want)
		}
	}
}

// A change to a section that carries credentials shows the change with the credentials masked. The
// answer goes into a chat log, and a section diff is exactly the place a credential would end up in
// one.
func TestAChangeToACredentialSectionAnswersWithTheCredentialsMasked(t *testing.T) {
	s, _ := newConfirmingSession(t, "accept")

	answered := call(t, s, "manage_config", map[string]any{
		"section": "access",
		"payload": []any{
			map[string]any{
				"id": float64(1), "code": listenerCode, "ident": listenerIdent, "systems": "*",
			},
			map[string]any{
				"id": float64(0), "code": "987654321", "ident": "volunteer", "systems": "*",
			},
		},
	})

	body, err := json.Marshal(answered.StructuredContent)
	if err != nil {
		t.Fatalf("the answer does not encode: %v", err)
	}

	for _, secret := range []string{listenerCode, "987654321"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("the answer hands out the access code %s: %s", secret, body)
		}
	}

	if !strings.Contains(string(body), maskedCredential) {
		t.Fatalf("the answer does not say which fields it masked: %s", body)
	}

	// The change itself is real: the code is stored, and only the answer is masked.
	if len(s.bundle.accesses.Stored) != 2 {
		t.Fatalf("the access list holds %d codes, want the fixture's and the new one: %+v",
			len(s.bundle.accesses.Stored), s.bundle.accesses.Stored)
	}
}

// The settings a submission does not name are reset to their defaults, because that is what the
// server's own settings save does. The ask names them: an assistant that sends two settings and
// tells the operator it is changing two is an assistant that quietly reset the rest.
func TestTheSettingsAChangeResetsAreNamed(t *testing.T) {
	s, operator := newConfirmingSession(t, "decline")

	refusedCall(t, s, "manage_config", map[string]any{
		"section": "options",
		"payload": map[string]any{"maxClients": float64(50)},
	})

	asked := operator.asked()

	if !strings.Contains(asked, "keypadBeeps") {
		t.Fatalf("the ask says %q, which does not name the keypad the change resets", asked)
	}
}

// A section this server does not have, a level with no system, and a payload of the wrong shape are
// refused before anything is asked: the capability cannot show a change it cannot make.
func TestAWriteAsksNothingAboutAChangeItCannotMake(t *testing.T) {
	for _, probe := range []struct {
		what string
		args map[string]any
	}{
		{"an unknown section", map[string]any{"section": "nope", "payload": []any{}}},
		{"a level with no system", map[string]any{"section": "talkgroups", "payload": []any{}}},
		{"a level with a system this server does not hold", map[string]any{
			"section": "talkgroups", "system": float64(99999), "payload": []any{},
		}},
		{"a list section with an object", map[string]any{"section": "groups", "payload": map[string]any{}}},
		{"the settings as a list", map[string]any{"section": "options", "payload": []any{}}},
		{"a setting this server does not have", map[string]any{
			"section": "options", "payload": map[string]any{"noSuchSetting": true},
		}},
		{"no payload at all", map[string]any{"section": "groups"}},
		{"an access add with no identity", map[string]any{"action": "add"}},
		{"an access remove with no identity", map[string]any{"action": "remove"}},
		{"an access add with no scope", map[string]any{"action": "add", "ident": "volunteer"}},
		{"an access add with a scope that is not a scope", map[string]any{
			"action": "add", "ident": "volunteer", "systems": "everything please",
		}},
		{"an access add with an expiry that is not an instant", map[string]any{
			"action": "add", "ident": "volunteer", "systems": "*", "expiration": "end of the year",
		}},
		{"an action this capability does not have", map[string]any{"action": "sideways", "ident": "volunteer"}},
		{"a password change with no new credential", map[string]any{"rotate": true}},
		{"a password change with an empty new credential", map[string]any{"newPassword": ""}},
	} {
		s, operator := newConfirmingSession(t, "accept")

		before := deploymentState(t, s.bundle)

		refusedOrRejected(t, s, probeToolOf(probe.args), probe.args)

		if len(operator.asks) != 0 {
			t.Fatalf("%s asked %d times: %s", probe.what, len(operator.asks), operator.asked())
		}

		if after := deploymentState(t, s.bundle); after != before {
			t.Fatalf("%s wrote to the deployment:\n%s\n%s", probe.what, before, after)
		}
	}
}

// refusedOrRejected is a capability saying no: by the protocol's own refusal, or by refusing the
// arguments before the capability runs. Both are the answer this test asks about — nothing was
// asked of the operator, and nothing was written.
func refusedOrRejected(t *testing.T, s *session, tool string, args map[string]any) string {
	t.Helper()

	answered, err := callTool(s, tool, args)
	if err != nil {
		return err.Error()
	}
	if !answered.IsError {
		t.Fatalf("%s answered %+v, wanting a refusal of a change it cannot make", tool, answered.StructuredContent)
	}

	return answeredText(answered)
}

// The write capabilities, with the arguments each one accepts against the fixture. The arguments
// have to be arguments the capability takes: the protocol validates them before the capability runs,
// and a probe whose arguments belong to another capability proves nothing about this one.
func writeCapabilityProbes() []struct {
	tool string
	args map[string]any
} {
	return []struct {
		tool string
		args map[string]any
	}{
		{"manage_config", map[string]any{
			"section": "options",
			"payload": map[string]any{"maxClients": float64(50)},
		}},
		{"manage_config", map[string]any{
			"section": "talkgroups",
			"system":  float64(systemOneRef),
			"payload": []any{map[string]any{"id": float64(0), "talkgroupRef": float64(1234), "label": "Fire Dispatch", "tagId": float64(3)}},
		}},
		{"manage_access_user", map[string]any{"action": "add", "ident": "volunteer", "systems": "*"}},
		{"manage_access_user", map[string]any{"action": "remove", "ident": "volunteer"}},
		{"rotate_admin_password", map[string]any{"newPassword": "a-long-new-administrator-credential"}},
	}
}

// An access-code caller is refused every write capability, and the refusal it gets is the refusal a
// caller carrying no credential gets for the same capability: reaching the deployment's writes is
// done by signing in as an administrator, and no weaker credential is read as a partial signature.
func TestTheAdministratorsWriteCapabilitiesRefuseAnAccessCodeCallerTheSameWayTheyRefuseACallerWithNoCredential(t *testing.T) {
	asListener := newAdministratorSetSession(t, readScoped())
	asAnonymous := newAdministratorSetSession(t, Caller{Tier: TierReadOnly})

	for _, probe := range writeCapabilityProbes() {
		listenerSaid := refusedCall(t, asListener, probe.tool, probe.args)
		anonymousSaid := refusedCall(t, asAnonymous, probe.tool, probe.args)

		if !strings.Contains(listenerSaid, "administrator") {
			t.Fatalf("%s refused an access-code caller with %q, which does not say what is required",
				probe.tool, listenerSaid)
		}

		if listenerSaid != anonymousSaid {
			t.Fatalf("%s refused the same caller differently by credential: access code %q, no credential %q",
				probe.tool, listenerSaid, anonymousSaid)
		}
	}
}

// The refusal above is the drift proof: a write capability standing in front of the caller is
// refused by its own handler. This is the shape the caller actually reaches — the capability set
// its credential opens — where the write capabilities are not offered at all and cannot be reached
// by naming them.
func TestTheReadOnlyTiersOfferNoWriteCapability(t *testing.T) {
	for _, who := range []Caller{readScoped(), {Tier: TierReadOnly}} {
		s := newCallerSession(t, who)

		listed, err := s.ListTools(context.Background(), &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("listing the capabilities: %v", err)
		}

		for _, tool := range listed.Tools {
			capability, ok := catalogOf(tool.Name)
			if !ok {
				t.Fatalf("%s is offered and is not in the catalog", tool.Name)
			}

			if capability.Effect == EffectWrite || capability.Effect == EffectDestructive {
				t.Fatalf("the read-only tier offers %s, a %s capability", tool.Name, capability.Effect)
			}
		}

		for _, probe := range writeCapabilityProbes() {
			before := deploymentState(t, s.bundle)

			refusedOrRejected(t, s, probe.tool, probe.args)

			if after := deploymentState(t, s.bundle); after != before {
				t.Fatalf("%s wrote to the deployment for a read-only caller:\n%s\n%s", probe.tool, before, after)
			}
		}
	}
}

// The administrator's journey: read a section the way the administrator's page reads it, write it
// back, write one level of one system, onboard a listener, remove them, and replace the credential.
// Each step goes over the entry point the capability names, and each answer is the acknowledgement
// of a change made — the surface's own rows, with the credentials masked, and never the server's
// domain values.
func TestTheAdministratorsWriteJourneyGoesOverTheEntryPointsItNames(t *testing.T) {
	s, _ := newConfirmingSession(t, "accept")

	// The settings, read as the resource answers them and written back with one setting changed.
	settings := readSection(t, s, optionsURI)

	payload := writableSettingsOf(settings)
	payload["maxClients"] = float64(50)

	answered := call(t, s, "manage_config", map[string]any{"section": "options", "payload": payload})

	assertAcknowledgement(t, "manage_config", answered)

	if got := s.bundle.options.Stored.MaxClients; got != 50 {
		t.Fatalf("the settings save left the listener ceiling at %d, want 50", got)
	}
	if got := s.bundle.options.Stored.PruneDays; got != 7 {
		t.Fatalf("the settings save moved the prune horizon to %d; the journey did not ask for it", got)
	}

	// One level of one system, read as the template answers it and written back with one entry
	// added. The other system is not this level's business and keeps what it had.
	levels := readRows(t, s, "rdio-scanner://systems/"+strconv.FormatUint(uint64(systemOneRef), 10)+"/talkgroups")

	levels = append(levels, map[string]any{
		"id": float64(0), "talkgroupRef": float64(1234), "label": "Fire Dispatch", "tagId": float64(3),
	})

	answered = call(t, s, "manage_config", map[string]any{
		"section": "talkgroups", "system": float64(systemOneRef), "payload": levels,
	})

	assertAcknowledgement(t, "manage_config", answered)

	first, _ := s.bundle.systems.Stored.ByRef(systemOneRef)
	second, _ := s.bundle.systems.Stored.ByRef(systemTwoRef)

	if len(first.Talkgroups) != 3 {
		t.Fatalf("system 1 holds %d talkgroups after the level write, want the fixture's two and the new one: %+v",
			len(first.Talkgroups), first.Talkgroups)
	}
	if len(second.Talkgroups) != 1 {
		t.Fatalf("the level write of system 1 left system 2 with %d talkgroups, want the one it had: %+v",
			len(second.Talkgroups), second.Talkgroups)
	}

	// A listener onboarded, then removed, over the same pair of entry points the page uses.
	answered = call(t, s, "manage_access_user", map[string]any{
		"action":     "add",
		"ident":      "volunteer",
		"systems":    []any{map[string]any{"systemRef": float64(systemOneRef), "talkgroups": []any{float64(1514000)}}},
		"expiration": "2026-12-31T00:00:00Z",
	})

	assertAcknowledgement(t, "manage_access_user", answered)

	if len(s.bundle.accesses.Stored) != 2 {
		t.Fatalf("the access list holds %d codes after the onboarding, want the fixture's and the new one: %+v",
			len(s.bundle.accesses.Stored), s.bundle.accesses.Stored)
	}

	onboarded := s.bundle.accesses.Stored[1]

	if onboarded.Ident != "volunteer" {
		t.Fatalf("the new code is filed under %q, want volunteer", onboarded.Ident)
	}
	if !onboarded.Scope.GrantsReferences(systemOneRef, 1514000) {
		t.Fatalf("the new code does not grant the scope that was named: %+v", onboarded.Scope)
	}
	if onboarded.Scope.GrantsReferences(systemTwoRef, 1514002) {
		t.Fatalf("the new code grants a system the journey never named: %+v", onboarded.Scope)
	}

	answered = call(t, s, "manage_access_user", map[string]any{"action": "remove", "ident": "volunteer"})

	assertAcknowledgement(t, "manage_access_user", answered)

	if len(s.bundle.accesses.Stored) != 1 {
		t.Fatalf("the access list holds %d codes after the removal, want only the fixture's: %+v",
			len(s.bundle.accesses.Stored), s.bundle.accesses.Stored)
	}

	// The credential, replaced. The server stops asking for the default one, and the answer never
	// carries the credential it replaced.
	answered = call(t, s, "rotate_admin_password", map[string]any{"newPassword": "a-long-new-administrator-credential"})

	assertAcknowledgement(t, "rotate_admin_password", answered)

	if s.bundle.options.Stored.AdminPasswordHash == "" {
		t.Fatal("the credential change left the server with no credential hash")
	}
	if s.bundle.options.Stored.AdminPasswordNeedChange {
		t.Fatal("the server still says the default credential is in place after the credential was replaced")
	}
}

// writeAnswerFields is the vocabulary each write capability answers in. An answer outside it is an
// answer carrying something the surface never declared: the server's own values, or a credential.
var writeAnswerFields = map[string][]string{
	"manage_config":         {"section", "system", "outcome", "current", "added", "changed", "removed", "settings", "reset", "saved", "note"},
	"manage_access_user":    {"action", "ident", "outcome", "added", "changed", "removed", "note"},
	"rotate_admin_password": {"outcome", "passwordNeedsChange", "note"},
}

// The names the server's domain values are spelled with. They are not this surface's vocabulary:
// an answer that carries one has handed the caller the server's persistence shape, with the fields
// a configuration payload does not name.
var domainSpellings = []string{
	"TalkgroupRef", "SiteRef", "UnitRef", "SystemRef", "GroupIDs", "TagID", "AdminPasswordHash",
	"Secret", "MaxClients", "PruneDays", "AutoPopulate", "KeypadBeeps", "ShowListenersCount",
	"SortTalkgroups", "DisableDuplicateDetection", "AudioConversion", "Expiration", "Scope",
	"SystemID", "DeleteAfter", "Directory", "ID",
}

// assertAcknowledgement is the shape a write capability's answer has to have: the outcome of the
// change, in the vocabulary the capability declared, with the credentials masked and the server's
// own values kept out of the answer.
func assertAcknowledgement(t *testing.T, tool string, answered *mcp.CallToolResult) {
	t.Helper()

	fields := map[string]json.RawMessage{}

	body, err := json.Marshal(answered.StructuredContent)
	if err != nil {
		t.Fatalf("%s answered content that is not the structured answer it declares: %v", tool, err)
	}

	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("%s answered content that is not the structured answer it declares: %v\n%s",
			tool, err, body)
	}

	if _, has := fields["outcome"]; !has {
		t.Fatalf("%s answered %s, which does not say what the server did", tool, body)
	}

	for name := range fields {
		declared := false

		for _, allowed := range writeAnswerFields[tool] {
			if name == allowed {
				declared = true

				break
			}
		}

		if !declared {
			t.Fatalf("%s answers with %s, which the capability does not declare", tool, name)
		}
	}

	text := string(body)

	for _, spelling := range domainSpellings {
		if strings.Contains(text, `"`+spelling+`"`) {
			t.Fatalf("%s answers with the server's own value %s: %s", tool, spelling, text)
		}
	}

	for _, credential := range []string{listenerCode, fixtureApikey, "a-long-new-administrator-credential"} {
		if strings.Contains(text, credential) {
			t.Fatalf("%s answers with the credential %s it touched", tool, credential)
		}
	}
}

// readSection is a configuration resource read the way an assistant reads it: the text the resource
// answers, which for a section is the section's rows and for the settings is the settings view.
func readSection(t *testing.T, s *session, uri string) map[string]any {
	t.Helper()

	answer, err := s.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("reading %s: %v", uri, err)
	}

	view := map[string]any{}

	if err := json.Unmarshal([]byte(answer.Contents[0].Text), &view); err != nil {
		t.Fatalf("%s is not the JSON it declares: %v\n%s", uri, err, answer.Contents[0].Text)
	}

	return view
}

// writableSettingsOf is the settings a caller can write back, read from the settings the server
// answered. The server's own notices are not settings, and a submission naming one is refused.
func writableSettingsOf(view map[string]any) map[string]any {
	payload := map[string]any{}

	for _, name := range writableSettings {
		if value, has := view[name]; has {
			payload[name] = value
		}
	}

	return payload
}

// readRows is a level resource read the way an assistant reads it: the list of rows the template
// answers, which is the shape the payload that writes the level back takes.
func readRows(t *testing.T, s *session, uri string) []any {
	t.Helper()

	answer, err := s.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("reading %s: %v", uri, err)
	}

	rows := []any{}

	if err := json.Unmarshal([]byte(answer.Contents[0].Text), &rows); err != nil {
		t.Fatalf("%s is not the list of rows it declares: %v\n%s", uri, err, answer.Contents[0].Text)
	}

	if len(rows) == 0 {
		t.Fatalf("%s answers no rows: %s", uri, answer.Contents[0].Text)
	}

	return rows
}

// probeToolOf is the capability a probe's arguments belong to: the access and password arguments
// name their own capability, and a section names the configuration one.
func probeToolOf(args map[string]any) string {
	if _, ok := args["action"]; ok {
		return "manage_access_user"
	}
	if _, ok := args["section"]; ok {
		return "manage_config"
	}

	return "rotate_admin_password"
}

// The ask is the sentence the operator decides on. The exploratory pass read the surface's own asks
// back to it, and three of them read wrong: the add named the server twice in one sentence, the
// removal counted one code in the plural, and a section replace named the server twice. All three are
// pinned here, because an operator who is asked to confirm a change has to be able to read the
// sentence in one pass.
func TestTheAccessCodeAskNamesOneServerOnce(t *testing.T) {
	s, operator := newConfirmingSession(t, "accept")

	call(t, s, "manage_access_user", map[string]any{
		"action":     "add",
		"ident":      "volunteer",
		"systems":    "*",
		"expiration": "2026-12-31T00:00:00Z",
	})

	if len(operator.asks) != 1 {
		t.Fatalf("the add asked %d times, want one ask: %s", len(operator.asks), operator.asked())
	}

	if n := strings.Count(operator.asks[0], "this server"); n != 1 {
		t.Fatalf("the add asked %q, which names the server %d times, want it named once", operator.asks[0], n)
	}
}

func TestTheRemovalAskCountsItsCodesInTheWordsTheCountNeeds(t *testing.T) {
	s, operator := newConfirmingSession(t, "accept")

	onboard := func(code string) {
		t.Helper()

		call(t, s, "manage_access_user", map[string]any{
			"action":     "add",
			"ident":      "volunteer",
			"code":       code,
			"systems":    "*",
			"expiration": "2026-12-31T00:00:00Z",
		})
	}

	onboard("700101")

	operator.asks = nil

	call(t, s, "manage_access_user", map[string]any{"action": "remove", "ident": "volunteer"})

	if len(operator.asks) != 1 {
		t.Fatalf("the removal of one code asked %d times, want one ask: %s", len(operator.asks), operator.asked())
	}

	if !strings.Contains(operator.asks[0], "remove 1 listener access code carrying") {
		t.Fatalf("the removal of one code asked %q, which counts one code in the plural", operator.asks[0])
	}

	onboard("700101")
	onboard("700102")

	operator.asks = nil

	call(t, s, "manage_access_user", map[string]any{"action": "remove", "ident": "volunteer"})

	if !strings.Contains(operator.asks[0], "remove 2 listener access codes carrying") {
		t.Fatalf("the removal of two codes asked %q, which does not count two codes in the plural", operator.asks[0])
	}
}

// The same sentence read twice: a section replace is stated as a replace of the section, on one
// server. The walk read the section asks back and found the server named twice in the sentence.
func TestTheSectionAskNamesOneServerOnce(t *testing.T) {
	s, operator := newConfirmingSession(t, "accept")

	rows := readRows(t, s, groupsURI)
	rows = append(rows, map[string]any{"id": float64(0), "label": "Mutual Aid", "alert": "", "led": ""})

	call(t, s, "manage_config", map[string]any{"section": "groups", "payload": rows})

	if len(operator.asks) != 1 {
		t.Fatalf("the section replace asked %d times, want one ask: %s", len(operator.asks), operator.asked())
	}

	if n := strings.Count(operator.asks[0], "this server"); n != 1 {
		t.Fatalf("the section replace asked %q, which names the server %d times, want it named once", operator.asks[0], n)
	}
}
