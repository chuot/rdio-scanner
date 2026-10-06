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
// along with this program.  If not, see <http://www.gnu.org/licenses/>.
//
// WebSocket API Access Policy:
// This WebSocket API is reserved exclusively for Saubeo Solutions and its native applications.
// Unauthorized access is strictly prohibited.
// See API_ACCESS_POLICY.md for full terms.

// capture.mjs drives a running legacy Rdio Scanner server through every part of the wire protocol
// the native applications and client-ng depend on, and writes what the server actually put on the
// wire into the golden directory.
//
// Run it through capture.sh, which builds the legacy server, starts it on a throwaway SQLite
// database, and stops it again. Run it by hand only against a server you started for this.
//
// Every golden file holds the bytes the legacy server wrote — a response body, or a whole
// WebSocket frame — with nothing added and nothing reformatted. Go marshals struct fields in
// declaration order and map keys sorted as strings, so a capture that was parsed and re-serialized
// by JavaScript is not the same bytes, and a golden that is not the same bytes proves nothing.
//
// Two things in a capture are not reproducible and are replaced, and nothing else is:
//
//   - the login token, which is signed with a secret the server generates at first startup;
//   - the moment a log entry was written.
//
// Both are replaced with a fixed placeholder named in the file that holds them. capture.sh runs the
// legacy server with TZ=UTC so that every moment the server renders from an instant is the same on
// every machine.

import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

const options = parseArgs(process.argv.slice(2));

const base = options.base;
const goldenDir = options.out;
const fixture = JSON.parse(readFileSync(options.fixtures, "utf8"));

const wsBase = base.replace(/^http/, "ws");

// The default administrator password a fresh database is seeded with.
const initialPassword = "rdio-scanner";

// The password the capture changes it to. The change is captured, and the server is thrown away.
const changedPassword = "capture-administrator-password";

// The recording every captured call carries: a wav header, and enough bytes after it to be a
// recording rather than a container header. The upload endpoints reject anything shorter.
const recording = Buffer.concat([
	Buffer.from(
		"524946462400000057415645666d74201000000001000100401f0000803e0000020010006461746100000000",
		"hex",
	),
	Buffer.from("a recording long enough to be a recording, and then some more of it"),
]);

// The moment the captured calls happened. The upload endpoints read a timestamp as milliseconds,
// and the server renders it in the time zone it runs in — which capture.sh fixes to UTC so that the
// rendered moment is the same on every machine.
const firstCallAt = Date.parse("2026-10-06T12:00:00Z");

// The access codes the restricted phase configures, and what each one is for.
const accesses = [
	{ id: 0, code: "capture-open", ident: "Capture open", systems: "*" },
	{
		id: 0,
		code: "capture-scoped",
		ident: "Capture scoped",
		systems: [{ id: 10001, talkgroups: "*" }],
	},
	{
		id: 0,
		code: "capture-expired",
		ident: "Capture expired",
		systems: "*",
		expiration: "1970-01-01T00:00:01Z",
	},
	{ id: 0, code: "capture-limited", ident: "Capture limited", limit: 1, systems: "*" },
];

const sections = [
	"access",
	"apikeys",
	"dirwatch",
	"downstreams",
	"groups",
	"options",
	"systems",
	"tags",
];

// ---------------------------------------------------------------------------
// Writing goldens
// ---------------------------------------------------------------------------

const written = [];

function save(name, body) {
	writeFileSync(path.join(goldenDir, name), body);

	written.push(name);
}

// redact replaces a value that cannot be reproduced — the login token — wherever it appears.
function redact(body, ...values) {
	let out = body;

	for (const value of values) {
		if (value.length === 0) {
			continue;
		}

		out = out.split(value).join("<token>");
	}

	return out;
}

// redactLogTimes replaces the moments a logs search reports: the moment each entry was written, and
// the first and last moments the search found across the whole result. Everything else about the
// logs capture is reproducible — it is taken at a fixed point of this script, and the level filter
// is what makes the number of entries fixed rather than a function of how the debounced listener
// count happened to land.
function redactLogTimes(body) {
	return body
		.replace(/"dateTime":"[^"]*"/g, '"dateTime":"<datetime>"')
		.replace(/"date(Start|Stop)":"[^"]*"/g, '"date$1":"<datetime>"');
}

// ---------------------------------------------------------------------------
// Talking to the server
// ---------------------------------------------------------------------------

async function rest(method, route, { token, body } = {}) {
	const headers = {};

	if (token !== undefined) {
		headers["Authorization"] = token;
	}

	if (body !== undefined) {
		headers["Content-Type"] = "application/json";
	}

	const res = await fetch(base + route, {
		method,
		headers,
		body: body === undefined ? undefined : JSON.stringify(body),
	});

	return { status: res.status, body: await res.text() };
}

async function upload(route, fields) {
	const form = new FormData();

	for (const [name, value] of Object.entries(fields)) {
		if (Buffer.isBuffer(value)) {
			form.append(name, new Blob([value], { type: "audio/wav" }), "capture.wav");
		} else if (typeof value === "object") {
			form.append(name, JSON.stringify(value));
		} else {
			form.append(name, String(value));
		}
	}

	const res = await fetch(base + route, { method: "POST", body: form });

	return { status: res.status, body: await res.text() };
}

function sleep(ms) {
	return new Promise((resolve) => setTimeout(resolve, ms));
}

// Session is one WebSocket connection, and the frames it received, kept exactly as they arrived.
class Session {
	constructor(label, route = "/") {
		this.label = label;
		this.frames = [];
		this.next = 0;

		this.conn = new WebSocket(wsBase + route);

		this.conn.addEventListener("message", (event) => this.frames.push(String(event.data)));

		this.opened = new Promise((resolve, reject) => {
			this.conn.addEventListener("open", resolve, { once: true });
			this.conn.addEventListener(
				"error",
				(event) => reject(new Error(`${label}: ${event.message ?? "websocket error"}`)),
				{ once: true },
			);
		});

		this.closed = new Promise((resolve) => this.conn.addEventListener("close", resolve));
	}

	async open() {
		await this.opened;
	}

	send(message) {
		this.conn.send(JSON.stringify(message));
	}

	// sendRaw writes the bytes as they are. The administrator's config socket is the one place in
	// the protocol where a frame is not a command array, and its first frame is the session token
	// itself — the server compares the frame against the token byte for byte, so a JSON-quoted
	// token is not that token.
	sendRaw(message) {
		this.conn.send(message);
	}

	// expect returns the next frame whose command matches, and fails the capture if none arrives: a
	// golden set that silently misses a command proves nothing about that command.
	async expect(command, timeout = 20000) {
		const deadline = Date.now() + timeout;

		while (Date.now() < deadline) {
			for (let i = this.next; i < this.frames.length; i++) {
				if (commandOf(this.frames[i]) === command) {
					this.next = i + 1;

					return this.frames[i];
				}
			}

			await sleep(20);
		}

		throw new Error(`${this.label}: no ${command} frame arrived, saw ${JSON.stringify(this.frames)}`);
	}

	// expectNothing waits out a window and fails the capture if the server says anything in it.
	// Several protocol cases are defined by the reply the server does not send.
	async expectNothing(within = 1500) {
		const seen = this.frames.length;

		await sleep(within);

		if (this.frames.length > seen) {
			throw new Error(`${this.label}: the server answered ${JSON.stringify(this.frames.slice(seen))}, want no answer`);
		}
	}

	// nextFrame returns the next frame the server sent, whatever it is. The administrator's config
	// socket is not command-framed: it is handed the configuration object itself.
	async nextFrame(timeout = 20000) {
		const deadline = Date.now() + timeout;

		while (Date.now() < deadline) {
			if (this.frames.length > this.next) {
				return this.frames[this.next++];
			}

			await sleep(20);
		}

		throw new Error(`${this.label}: no frame arrived`);
	}

	close() {
		this.conn.close();

		return this.closed;
	}
}

function commandOf(frame) {
	return frame.slice(2, frame.indexOf('"', 2));
}

// payloadOf returns the payload of a frame as the server wrote it. The frame is scanned rather than
// parsed, so the bytes that come out are the bytes that went in.
function payloadOf(frame) {
	const command = commandOf(frame);
	const start = command.length + 4;

	if (frame.slice(0, start) !== `["${command}",`) {
		throw new Error(`the frame ${frame.slice(0, 40)} does not start with a ${command} payload`);
	}

	const end = jsonValueEnd(frame, start);

	if (end < 0) {
		throw new Error(`the frame ${frame.slice(0, 40)} has no payload that ends`);
	}

	return frame.slice(start, end);
}

// jsonValueEnd finds the end of the JSON value starting at `start`, counting brackets and skipping
// over strings.
function jsonValueEnd(text, start) {
	let depth = 0;
	let inString = false;
	let escaped = false;

	for (let i = start; i < text.length; i++) {
		const ch = text[i];

		if (inString) {
			if (escaped) {
				escaped = false;
			} else if (ch === "\\") {
				escaped = true;
			} else if (ch === '"') {
				inString = false;
			}

			continue;
		}

		if (ch === '"') {
			inString = true;
		} else if (ch === "{" || ch === "[") {
			depth++;
		} else if (ch === "}" || ch === "]") {
			depth--;

			if (depth === 0) {
				return i + 1;
			}
		}
	}

	return -1;
}

function framesOf(session) {
	return session.frames;
}

// ---------------------------------------------------------------------------
// The capture
// ---------------------------------------------------------------------------

mkdirSync(goldenDir, { recursive: true });

// --- the administrator -----------------------------------------------------

const login = await rest("POST", "/api/admin/login", { body: { password: initialPassword } });

const token = JSON.parse(login.body).token;

save("rest-login.json", redact(login.body, token));

// The whole-configuration save, which is the one save the legacy server has always had.
await rest("PUT", "/api/admin/config", { token, body: fixture });

const config = await rest("GET", "/api/admin/config", { token });

save("rest-config.json", config.body);

const alerts = await rest("GET", "/api/admin/alerts", { token });

save("rest-alerts.json", alerts.body);

for (const section of sections) {
	const got = await rest("GET", `/api/admin/config/${section}`, { token });

	save(`rest-section-${section}.json`, got.body);
}

// A per-section save, captured because client-ng saves one section at a time and the reply it reads
// back is that section, not the whole configuration.
const optionsPut = await rest("PUT", "/api/admin/config/options", { token, body: fixture.options });

save("rest-section-options-put.json", optionsPut.body);

// The logs, taken here rather than at the end, and at one level rather than all of them: this is
// the point where the log holds only the lines this script caused, and the level filter is what
// makes their number fixed rather than a function of how the debounced listener count happened to
// land. An unfiltered search would return whatever the server had said in the meantime, which is
// not something a golden can be compared against.
const logs = await rest("POST", "/api/admin/logs", {
	token,
	body: { level: "warn", limit: 200, offset: 0, sort: 0 },
});

save("rest-logs.json", redactLogTimes(logs.body));

// The password is changed at the very end of this script, not here: changing it makes the server
// read the options back out of the database, and the audio-conversion setting is not one of the
// options the server ever writes there, so doing it earlier would silently switch conversion back
// on and hand the captures audio produced by whatever ffmpeg this machine has.

// --- the call upload endpoints ---------------------------------------------

const uploaded = await upload("/api/call-upload", {
	key: "capture-apikey-open",
	system: 10001,
	talkgroup: 1514000,
	talkgroupLabel: "Ground",
	talkgroupName: "Dispatch 1",
	timestamp: firstCallAt,
	site: 101,
	frequencies: [{ dbm: -70, errorCount: 1, freq: 151400000, pos: 0.5, spikeCount: 2 }],
	patches: [1514100],
	sources: [{ pos: 0.5, src: 1001, tag: "Car 1" }, { pos: 1.5, src: 1002, tag: "Car 2" }],
	audio: recording,
	audioFilename: "capture.wav",
	audioMime: "audio/wav",
});

save("rest-upload.txt", uploaded.body);

await upload("/api/call-upload", {
	key: "capture-apikey-open",
	system: 10002,
	talkgroup: 1515000,
	timestamp: firstCallAt + 60000,
	frequency: 151500000,
	audio: recording,
});

// An apikey the server does not know. A key scoped to specific systems is not tried here: the
// upload endpoint checks the key before the call's system has been resolved, and a scoped key
// dereferences that unresolved system, which ends the connection with no reply at all. Recorded as
// quirk Q51.
const denied = await upload("/api/call-upload", {
	key: "not-an-apikey",
	system: 10002,
	talkgroup: 1515000,
	timestamp: firstCallAt + 120000,
	audio: recording,
});

save("rest-upload-denied.txt", denied.body);

const trunkRecorder = await upload("/api/trunk-recorder-call-upload", {
	key: "capture-apikey-open",
	meta: {
		freqList: [{ error_count: 1, freq: 151400000, pos: 0.5, spike_count: 2 }],
		patched_talkgroups: [1514100],
		short_name: "Grand Metro",
		srcList: [{ pos: 0.5, src: 1001 }],
		talkgroup: 1514000,
		talkgroup_description: "Dispatch 1",
		talkgroup_group: "Ground",
		talkgroup_group_tag: "Fire Dispatch",
		talkgroup_tag: "Ground",
		timestamp: firstCallAt + 180000,
	},
	audio: recording,
});

save("rest-trunk-recorder-upload.txt", trunkRecorder.body);

// --- the listener, on a server that asks for no access code ----------------

const open = new Session("open");

await open.open();

open.send(["VER"]);

const ver = await open.expect("VER");

save("payload-ver.json", payloadOf(ver));

open.send(["CFG"]);

const cfg = await open.expect("CFG");

save("payload-cfg.json", payloadOf(cfg));

open.send(["LFM", { 10001: { 1514000: true } }]);

await open.expect("LFM");

// A listener is not counted when it arrives. It is counted five seconds after the last listener
// arrived or left, so the count is waited for here, before anything else registers or unregisters,
// so that the order the frames arrive in is the order this script asks for them in.
await open.expect("LSC");

// A call that arrives while the listener is watching that system and talkgroup is pushed to it.
await upload("/api/call-upload", {
	key: "capture-apikey-open",
	system: 10001,
	talkgroup: 1514000,
	timestamp: firstCallAt + 240000,
	audio: recording,
});

const live = await open.expect("CAL");

save("payload-cal-live.json", payloadOf(live));

open.send(["LCL", { limit: 200, offset: 0, sort: "date" }]);

const lcl = await open.expect("LCL");

save("payload-lcl.json", payloadOf(lcl));

// The identifiers the restricted phase needs, read out of the search this listener just did. In a
// search result the system is the system reference itself, not an object holding one.
const callId = JSON.parse(payloadOf(live)).id;
const otherCallId = JSON.parse(payloadOf(lcl)).results.find((call) => call.system !== 10001).id;

// A call the listener asks for, and the flag it asked with.
open.send(["CAL", String(callId), "p"]);

const fetched = await open.expect("CAL");

if (!fetched.endsWith(',"p"]')) {
	throw new Error(`the fetched call does not carry the flag the listener asked with: ${fetched.slice(-20)}`);
}

save("payload-cal-fetch.json", payloadOf(fetched));

const second = new Session("second");

await second.open();

second.send(["CFG"]);

await second.expect("CFG");

await open.expect("LSC");
await second.expect("LSC");

// A configuration change reaches the listener without being asked for, together with the count.
await rest("PUT", "/api/admin/config/tags", { token, body: fixture.tags });

await open.expect("CFG");
await open.expect("LSC");
await second.expect("CFG");
await second.expect("LSC");

// The count is switched off before the restricted phase, so that every session that registers from
// there on is not followed by a delayed count reaching sessions whose frames are being captured.
await rest("PUT", "/api/admin/config/options", { token, body: { ...fixture.options, showListenersCount: false } });

open.send(["LFM", null]);

await open.expect("LFM");

await open.close();
await second.close();

// The last delayed count is allowed to expire harmlessly before the restricted phase begins.
await sleep(6000);

save("ws-frames-open.json", JSON.stringify({ listener: framesOf(open), second: framesOf(second) }, null, 2) + "\n");

// --- the administrator's config socket -------------------------------------

const admin = new Session("admin", "/api/admin/config");

await admin.open();

admin.sendRaw(token);

// The connection is put into the set of connections to push to by a loop of its own, and the push
// itself happens on a goroutine the config save starts. The change is made after the connection has
// been handed over so that the push has a connection to find.
await sleep(500);

await rest("PUT", "/api/admin/config/groups", { token, body: fixture.groups });

save("adminws-config.json", await admin.nextFrame());

await admin.close();

// A config socket that presents a token the server does not recognize is closed by the server. It
// is the only way the socket authenticates: the upgrade itself asks for nothing.
const refusedSocket = new Session("refused-socket", "/api/admin/config");

await refusedSocket.open();

refusedSocket.sendRaw("not-a-token");

const refusedSocketClose = await refusedSocket.closed;

save("adminws-bad-token.json", JSON.stringify({ code: refusedSocketClose.code, reason: refusedSocketClose.reason }, null, 2) + "\n");

// --- the listener, on a server that asks for an access code ----------------

const restricted = await rest("PUT", "/api/admin/config/access", { token, body: accesses });

save("rest-section-access-restricted.json", restricted.body);

const scoped = new Session("scoped");

await scoped.open();

scoped.send(["VER"]);

// The version is answered even before the access code: a client that has unlocked nothing still
// learns what server it is talking to.
await scoped.expect("VER");

for (const command of ["CFG", "LCL", "CAL", "LFM"]) {
	scoped.send([command]);

	await scoped.expect("PIN");
}

scoped.send(["PIN", Buffer.from("not-an-access-code").toString("base64")]);

await scoped.expect("PIN");

scoped.send(["PIN", Buffer.from("capture-scoped").toString("base64")]);

const scopedCfg = await scoped.expect("CFG");

save("payload-cfg-scoped.json", payloadOf(scopedCfg));

// A call on a system this access does not cover gets no answer at all.
scoped.send(["CAL", String(otherCallId), "p"]);

await scoped.expectNothing();

// A call on a system it does cover is handed over.
scoped.send(["CAL", String(callId), "d"]);

await scoped.expect("CAL");

// A configuration change on a restricted server does not push the configuration: it pushes the
// request to unlock again.
await rest("PUT", "/api/admin/config/tags", { token, body: fixture.tags });

await scoped.expect("PIN");

await scoped.close();

const expired = new Session("expired");

await expired.open();

expired.send(["PIN", Buffer.from("capture-expired").toString("base64")]);

await expired.expect("XPR");

await expired.close();

// The connection limit is counted against the access code, and a code limited to one allows two
// connections and refuses the third.
const limited = [];

for (const label of ["limited-1", "limited-2", "limited-3"]) {
	const session = new Session(label);

	await session.open();

	session.send(["PIN", Buffer.from("capture-limited").toString("base64")]);

	await session.expect(label === "limited-3" ? "MAX" : "CFG");

	limited.push(session);
}

for (const session of limited) {
	await session.close();
}

save("ws-frames-restricted.json", JSON.stringify({
	scoped: framesOf(scoped),
	expired: framesOf(expired),
	limited: limited.map(framesOf),
}, null, 2) + "\n");

// Five attempts on one connection is the end of that connection: the fifth is refused even when the
// code it names is a valid one.
const locked = new Session("locked");

await locked.open();

for (let attempt = 1; attempt <= 4; attempt++) {
	locked.send(["PIN", Buffer.from("not-an-access-code").toString("base64")]);

	await locked.expect("PIN");
}

locked.send(["PIN", Buffer.from("capture-open").toString("base64")]);

await locked.expect("PIN");

await locked.close();

save("ws-frames-locked.json", JSON.stringify({ locked: framesOf(locked) }, null, 2) + "\n");

// --- the administrator's own sessions --------------------------------------

const password = await rest("POST", "/api/admin/password", {
	token,
	body: { currentPassword: initialPassword, newPassword: changedPassword },
});

save("rest-password.json", password.body);

const logout = await rest("POST", "/api/admin/logout", { token });

save("rest-logout-status.txt", `${logout.status}\n`);

const refused = await rest("GET", "/api/admin/config", { token: "not-a-token" });

save("rest-config-unauthorized-status.txt", `${refused.status}\n`);

// --- a call that is not there ----------------------------------------------

// This is the last thing the capture does, on purpose. Asking for a call that does not exist makes
// the legacy server abandon a database transaction without ending it, which locks every writer out
// for ten seconds and then makes them fail with "database is locked": doing it earlier stalls the
// replies and the saves that follow it. Recorded as quirk Q52.
const missing = new Session("missing");

await missing.open();

missing.send(["PIN", Buffer.from("capture-open").toString("base64")]);

await missing.expect("CFG");

missing.send(["CAL", "999999", "p"]);

await missing.expectNothing();

await missing.close();

save("ws-frames-missing-call.json", JSON.stringify({ missing: framesOf(missing) }, null, 2) + "\n");

// ---------------------------------------------------------------------------

console.log(`captured ${written.length} golden files into ${goldenDir}`);

function parseArgs(argv) {
	const parsed = { out: "testdata/golden", fixtures: "testdata/capture/fixtures/config.json" };

	for (let i = 0; i < argv.length; i++) {
		switch (argv[i]) {
			case "--base":
				parsed.base = argv[++i];
				break;
			case "--out":
				parsed.out = argv[++i];
				break;
			case "--fixtures":
				parsed.fixtures = argv[++i];
				break;
			default:
				throw new Error(`unknown argument ${argv[i]}`);
		}
	}

	if (!parsed.base) {
		throw new Error("--base is required");
	}

	return parsed;
}
