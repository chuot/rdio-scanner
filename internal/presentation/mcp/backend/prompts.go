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

package backend

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The administrator's standing questions, packaged so a client starts a
// session with them: a prompt states the question, the capabilities that
// answer it, and their order, registered on the administrator's set.
// see the prompts-carry-standing-questions-for-administrators maintainer note.

// registerStandingQuestions adds the administrator's standing questions.
func registerStandingQuestions(target *mcp.Server, deps Deps) {
	target.AddPrompt(&mcp.Prompt{
		Name:  "daily_health_review",
		Title: "Is this deployment healthy?",
		Description: "One day of this deployment reviewed as a whole, with the log lines behind the counts read " +
			"and the findings named as the decisions they need.",
		Arguments: []*mcp.PromptArgument{{
			Name:        "date",
			Description: "the day to review, as a date or an RFC 3339 instant inside it; defaults to the day this server is in",
		}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		day := dayOf(argument(req, "date"))
		if day == "" {
			day = dayOf(deps.Clock.Now().Format(time.RFC3339))
		}

		return answer("Reviewing "+day+", the deployment as a whole.",
			"Review the health of this rdio-scanner deployment on "+day+".\n\n"+
				"1. Call analyze_health with date "+day+". It composes the log view asked once per severity, "+
				"the server's own restart markers, the watched directories and downstream servers that are "+
				"switched off, the retention horizon, the listener ceiling, and the notice that the "+
				"administrator password still needs changing, into the findings that need a decision.\n"+
				"2. Call search_logs twice for "+day+": once with level \"warn\" and once with level \"error\", "+
				"oldest first. Read the lines behind the counts. The review counts with the log view's own "+
				"per-subsystem tally, so a count with no line behind it is a count of lines the page did not "+
				"carry, and saying so is the honest answer.\n"+
				"3. Name what needs a decision. For each finding, name the subsystem, say what the lines say, "+
				"and name the capability that changes it. Say nothing about a subsystem the review found "+
				"nothing about.\n\n"+
				"The review reads one day and says which day it read. A server that came up more than once in "+
				"the day has a reason an operator has to go and find: name the restart markers, and do not "+
				"report them as a subsystem warning.")
	})

	target.AddPrompt(&mcp.Prompt{
		Name:  "troubleshoot_silent_talkgroup",
		Title: "This talkgroup has gone quiet. Why?",
		Description: "One talkgroup read from every side at once, and the two fixes worth trying, in the order " +
			"they are worth trying.",
		Arguments: []*mcp.PromptArgument{
			{Name: "system", Description: "the system's reference, as list_systems names it", Required: true},
			{Name: "talkgroup", Description: "the talkgroup's reference", Required: true},
			{Name: "since", Description: "roughly when it last reported, as a date; it narrows what \"quiet\" means"},
		},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		system, talkgroup, err := requirePair(req)
		if err != nil {
			return nil, err
		}

		since := argument(req, "since")

		extra := ""
		if since != "" {
			extra = "\n\nThe operator says it last reported around " + since +
				". Treat anything older than that as quiet, and say how long it has been quiet."
		}

		return answer("Diagnosing talkgroup "+talkgroup+" on system "+system+".",
			"Talkgroup "+talkgroup+" on system "+system+" has gone quiet. Find out why, and propose the two "+
				"likeliest fixes.\n\n"+
				"1. Call diagnose_feed_problem with system "+system+" and talkgroup "+talkgroup+". It answers "+
				"the last recorded call on that talkgroup and how much history it has, the log lines naming "+
				"it, the system's delay, auto-populate, and blacklist count, the watched directories feeding "+
				"that system and whether they are switched off, and the downstream servers whose scope "+
				"forwards it.\n"+
				"2. Read the answer in this order. A watched directory feeding the system that is switched off "+
				"is ingestion, not the talkgroup. Log lines naming the talkgroup name a cause. A system with "+
				"auto-populate off explains a history that stopped being filled in. A downstream whose scope "+
				"does not grant this talkgroup explains recordings that never arrive at the peer.\n"+
				"3. Propose exactly two fixes, in the order you would try them. For each, name the capability "+
				"that makes it — manage_config for a system's settings, a watched directory, or a downstream "+
				"— and say what the change is and what it costs. Do not write anything: the operator asks for "+
				"the fix, and the change is made in the confirmed round."+extra)
	})

	target.AddPrompt(&mcp.Prompt{
		Name:  "explain_recent_errors",
		Title: "Explain the errors of the last few hours",
		Description: "Each distinct error of the window, explained by what its own subsystem said around it and " +
			"what the other subsystems said in the same window.",
		Arguments: []*mcp.PromptArgument{{
			Name:        "hours",
			Description: "how far back to look, in hours; defaults to 24",
		}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		hours := argument(req, "hours")
		if hours == "" {
			hours = "24"
		}

		return answer("Explaining the errors of the last "+hours+" hours.",
			"Explain the errors this rdio-scanner server has written in the last "+hours+" hours.\n\n"+
				"1. Call search_logs with level \"error\", oldest first, for the days the window falls in. The "+
				"log view's own criterion is a day, so the window is read as the days it falls in, and the "+
				"lines outside the window are not part of the answer.\n"+
				"2. Take the distinct errors. The same subsystem failing on several calls is one error with "+
				"several lines, not several errors.\n"+
				"3. Call explain_error once for each distinct error, naming it by its subsystem and a substring "+
				"of its message. The answer is the neighbourhood: the lines that subsystem wrote that day in "+
				"order on both sides of it, and the lines the other subsystems wrote in the same window, "+
				"grouped by subsystem.\n"+
				"4. For each error, say what the neighbourhood shows about the cause, and stop at the cause the "+
				"lines support. A line naming a subsystem that is the last thing written before the server's "+
				"own startup line is a restart, and the restart is what to explain.")
	})

	target.AddPrompt(&mcp.Prompt{
		Name:  "audit_access_codes",
		Title: "Audit the access codes",
		Description: "The listener access codes read as an inventory: which ones are expired, which grant " +
			"everything, which name no scope, and where a code's use is answered.",
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return answer("Auditing the listener access codes.",
			"Audit the access codes of this rdio-scanner deployment.\n\n"+
				"1. Read the resource "+accessURI+". It answers one row per code: whose code it is, when it "+
				"stops working, whether it is expired against this server's own clock, how many listeners it "+
				"lets in at once, and which systems and talkgroups it grants.\n"+
				"2. The codes themselves are marked as masked, and they stay masked. Do not ask for them, do "+
				"not print one, and do not repeat one back.\n"+
				"3. Name the codes the operator should act on: the expired ones; the ones granting everything "+
				"(systems \"*\"); the ones naming no scope at all, which is a code that sees nothing; and the "+
				"ones switched off.\n"+
				"4. The inventory carries no record of when a code was last used, and it does not pretend to. "+
				"Where use is the question, answer it from the log view: call search_logs with source "+
				"\"access\" and the code's identity as the message text, and say what the lines show — and say "+
				"when the log view holds nothing for that identity.\n"+
				"5. Name removal as what it is: removing a listener by identity deletes every code sharing "+
				"that name. Say which codes will go, by name, before anything is removed.")
	})

	target.AddPrompt(&mcp.Prompt{
		Name:  "onboard_listener",
		Title: "Give a listener an access code",
		Description: "The exact write call for a new access code, with the scope spelled out system by system, " +
			"asked before anything is written.",
		Arguments: []*mcp.PromptArgument{
			{Name: "ident", Description: "the identity the code belongs to, as the operator names it", Required: true},
			{Name: "systems", Description: "what the code may see: systemRef, systemRef:*, or systemRef:talkgroup, several separated by commas; a bare reference means every talkgroup on that system", Required: true},
			{Name: "expires", Description: "when the code stops working, as an instant the configuration takes, like 2026-12-31T00:00:00Z", Required: true},
		},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		ident, systems, expires, err := requireOnboarding(req)
		if err != nil {
			return nil, err
		}

		scope, err := spellScope(systems)
		if err != nil {
			return nil, err
		}

		return answer("Onboarding "+ident+", with the scope spelled out.",
			"Give "+ident+" an access code on this rdio-scanner deployment, expiring at "+expires+".\n\n"+
				"1. Read the resource "+accessURI+" first, so the identity is not already taken and so the "+
				"scope being asked for is not already granted by a code that exists.\n"+
				"2. Call manage_access_user with the add form: identity "+ident+", expiration "+expires+
				", and the scope spelled out below. The scope is the systems the code may see, each with "+
				"the talkgroups under it; the star means every talkgroup on that system. Do not name a "+
				"code: the server generates one, and no answer of the tool carries it.\n"+
				"   "+scope+"\n"+
				"3. Nothing is written by this prompt. manage_access_user answers the change it intends to "+
				"make and asks the operator; report what it says it will change, and write only in the "+
				"confirmed round, when the operator confirms it.\n"+
				"4. Say what the code costs: it is a credential. An access code repeated into a chat log "+
				"is a leaked credential, and the code is read back from the administrator's access page "+
				"by the operator, for that reason.")
	})
}

// answer is a standing question's one instruction, on the user's turn: the
// protocol hands a prompt's messages to the assistant as the user's own
// words.
func answer(description, instruction string) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: description,
		Messages: []*mcp.PromptMessage{{
			Role:    "user",
			Content: &mcp.TextContent{Text: instruction},
		}},
	}, nil
}

// argument is one argument of a standing question, as the client sent it.
func argument(req *mcp.GetPromptRequest, name string) string {
	return strings.TrimSpace(req.Params.Arguments[name])
}

// requirePair reads the system and talkgroup a talkgroup question cannot be
// asked without. The protocol checks the arguments a prompt declares, so
// the question checks its own, and the refusal names what is missing.
func requirePair(req *mcp.GetPromptRequest) (string, string, error) {
	system := argument(req, "system")
	if system == "" {
		return "", "", newRefusal("this question needs the system argument: the reference of the system the talkgroup is on")
	}

	talkgroup := argument(req, "talkgroup")
	if talkgroup == "" {
		return "", "", newRefusal("this question needs the talkgroup argument: the reference of the talkgroup that has gone quiet")
	}

	return system, talkgroup, nil
}

// requireOnboarding reads what the onboarding question cannot be asked
// without: whose code it is, what it may see, and when it stops working.
func requireOnboarding(req *mcp.GetPromptRequest) (string, string, string, error) {
	ident := argument(req, "ident")
	if ident == "" {
		return "", "", "", newRefusal("this question needs the ident argument: the identity the access code belongs to")
	}

	systems := argument(req, "systems")
	if systems == "" {
		return "", "", "", newRefusal("this question needs the systems argument: what the access code may see")
	}

	expires := argument(req, "expires")
	if expires == "" {
		return "", "", "", newRefusal("this question needs the expires argument: when the access code stops working, as seconds since 1970")
	}

	if _, err := strconv.ParseUint(expires, 10, 64); err != nil {
		return "", "", "", newRefusal("the expires argument is not a number of seconds since 1970")
	}

	return ident, systems, expires, nil
}

// spellScope spells the scope an operator names out system by system into
// the scope the write call takes, so the change is read before it is
// made and not invented at send time: the domain's rule holds, a system
// named without talkgroups is unseen, so the bare reference names all.
func spellScope(systems string) (string, error) {
	entries := []string{}

	for _, field := range strings.Split(systems, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}

		ref, talkgroups, err := scopeEntry(field)
		if err != nil {
			return "", err
		}

		entries = append(entries, ref+talkgroups)
	}

	if len(entries) == 0 {
		return "", newRefusal("the systems argument names no system: name systemRef, systemRef:*, or systemRef:talkgroup, several separated by commas")
	}

	return "[" + strings.Join(entries, ", ") + "]", nil
}

// scopeEntry spells one system of the scope. The star is the star; a list of talkgroups is the list.
func scopeEntry(field string) (string, string, error) {
	ref, list, found := strings.Cut(field, ":")

	ref = strings.TrimSpace(ref)

	if _, err := strconv.ParseUint(ref, 10, 32); err != nil {
		return "", "", newRefusal(fmt.Sprintf("the systems argument names %q, which is not a system reference", ref))
	}

	if !found || strings.TrimSpace(list) == "*" {
		return `"systemRef": ` + ref, `, "talkgroups": "*"`, nil
	}

	refs := []string{}

	for _, talkgroup := range strings.Split(list, "+") {
		talkgroup = strings.TrimSpace(talkgroup)

		if _, err := strconv.ParseUint(talkgroup, 10, 32); err != nil {
			return "", "", newRefusal(fmt.Sprintf(
				"the systems argument names %q on system %s, which is not a talkgroup reference", talkgroup, ref))
		}

		refs = append(refs, talkgroup)
	}

	return `"systemRef": ` + ref, `, "talkgroups": [` + strings.Join(refs, ", ") + `]`, nil
}

// The standing questions are the administrator's, and the inventory names
// them behind the administrator's credential, so the two cannot drift
// apart.
func standingQuestionNames() []string {
	names := []string{}

	for _, capability := range capabilityCatalog {
		if capability.Kind == KindPrompt && capability.Reach == ReachAdministrator {
			names = append(names, capability.Name)
		}
	}

	sort.Strings(names)

	return names
}
