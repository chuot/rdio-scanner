# The server log

The server writes down what it does, and every entry says **which part of the server** wrote it. That is the
whole point of the log: on a deployment that is misbehaving, you do not read everything the server said — you
ask for what the directory watchers said, or what the last hour of logins said, and read only that.

The log is in three places, and they are the same log:

- the **Logs** section of the administration interface, at `http://<host>:<port>/admin/`;
- the server's console, which is what `journalctl -u rdio-scanner` shows on a service deployment;
- the `logs` table of the database.

## Every entry names its subsystem

| Subsystem | Shown in the log view as | What it covers |
| --- | --- | --- |
| `server` | Server | The server starting, the base folder it serves, and the server stopping — including an address it could not stop answering |
| `config` | Configuration | A configuration you saved, a configuration that could not be saved, and a system, talkgroup, group, tag or unit the server created because a recording named it |
| `dirwatch` | Directory watchers | The watched recording directories: starting a watch, losing one, reading one, and handing a recording to the ingest path |
| `downstream` | Downstreams | Sending a call to another Rdio Scanner instance |
| `delayer` | Delayed calls | Calls held back by the **Delayed** feature: holding one, releasing one, and failing to do either |
| `ingest` | Call ingest | A recording becoming a call, and a recording that matches no system or talkgroup the server knows |
| `login` | Logins | A refused attempt, too many attempts from one address, a sign-in or sign-out the server could not carry out, and signing out. A sign-in that worked writes nothing — the administration app's own connection is what records that an administrator arrived, under **Administration**. |
| `access` | Access codes | A client presenting an access code that is not valid, expired or locked, or one already at its connection limit |
| `livefeed` | Live feed | Listeners joining and leaving, how many there are, and what the server could not read from or write to one |
| `ffmpeg` | Audio conversion | Converting a recording to playable audio |
| `upload` | Uploads | An upload the server refused, and the reason it refused it |
| `prune` | Retention | The retention pass, how much it removed, and what stopped it removing anything |
| `logs` | Log search | The log view failing to answer a search |
| `database` | Database | The database migration: what it migrated, what it refused, and what it left damaged |
| `admin` | Administration | The administrator account: a password changed or not changed, a user added or removed, a new signing secret, the administration app's own connection — joined, ended, or presenting a session token this server did not issue — and a request from the administrator whose body the server could not read |
| `mcp` | Assistant surface | The Model Context Protocol endpoint, when it is enabled: a caller it would not let in, named by the address the request came from; a capability refusing a caller, named by the capability and the identity the caller's credential is filed under; a capability that changed the deployment; and the endpoint's own transport trouble. It files no entry for a read an assistant was entitled to, and no entry ever carries a credential |
| `calls` | Calls | A call an operator acted on, such as pinning one. The current server writes nothing under this name; on a database upgraded from the previous server it holds the entries that server wrote |
| `legacy` | From the previous server | Entries that arrived before this field existed, or from a server that does not write it |

Those names are a closed list. Asking the log view for a subsystem the server does not have is answered as a
bad request, not as an empty log: an empty log would tell you nothing happened, when what happened is that you
asked for something this server cannot report.

## What an entry looks like

In the log view, the message says what happened and nothing else:

```
the watcher for /data/rdio/watch/trunk stopped: no such file or directory
```

On the console and in the service log, the same entry is said with its level and its subsystem in front of it,
so the subsystem split reaches the deployments that are diagnosed from a terminal:

```
warn dirwatch: the watcher for /data/rdio/watch/trunk stopped: no such file or directory
```

That is what you grep for:

```console
$ journalctl -u rdio-scanner | grep ' dirwatch: '
```

The levels are `info`, `warn` and `error`. A line that only ever went to the console — the base folder the
server serves, the server stopping, a new signing secret, and everything the server said about a listener
connection it could not read, could not understand, or could not write to — is now written to the log as well,
so it is searchable instead of being something you had to be watching when it happened. The three notices that
name the addresses the server is listening on are still console only: they say where to point a browser, not
what the server did.

## The prefixes are gone

Until now, a stored message began with the name of the internal function that wrote it, and diagnosing meant
knowing those names. They are gone. The message says what happened, in the language of the system, and where
something failed the failure's own text follows it.

| Before | Now |
| --- | --- |
| `newcall: system=10001 talkgroup=1514000 file=call.wav success` | `call from system 10001 talkgroup 1514000 success, call.wav` |
| `dirwatch.watcher: no such file or directory` | `the watcher for /data/rdio/watch/trunk stopped: no such file or directory` |
| `invalid login attempt for ip 203.0.113.7` | `a login attempt from 203.0.113.7 was refused` |
| `admin.passwordhandler.post: unable to change admin password, current password is invalid` | `the administrator password was not changed: current password is invalid` |
| `delayer.pop: no such file or directory` | `the held calls could not be released: no such file or directory` |
| `listeners count is 1` | `there is 1 listener` |
| `database pruning` | `the retention pass removed 1204 calls and 318 log entries` |

Entries written by the previous server were not rewritten. They were filed under the subsystem their old prefix
names — `invalid login attempt` became a `login` entry, `dirwatch.walkdir:` a `dirwatch` entry — and anything
that matches no prefix is shown as **From the previous server**. So a log you had before the upgrade is still
readable, and still filterable, in the log view.

## Searching the log

The log view has six controls and two buttons.

| Control | What it does |
| --- | --- |
| **Sort order** | Oldest first or newest first. Newest first is the default, and is what you want when something just broke. |
| **Date** | Narrows to one day, and the window depends on the sort order. This is the previous server's rule, kept exactly as it was. **Oldest first** takes the minute you chose and runs forward a full day. **Newest first** runs back a day from the minute you chose and then back again by that same hour and minute, so the window is shorter than a day and usually lands on the day *before* the one you typed. An empty table after choosing today is normally this, not a log with nothing in it: try the other sort order, or the same date at `00:00`. |
| **Level** | `Informative`, `Warning` or `Error`, or all of them. |
| **Subsystem** | One of the names above, or all of them. |
| **Text** | A substring of the message, matched without regard to case. A `%` or an `_` you type matches itself. |
| **Auto-refresh** | Keeps the view current, once every ten seconds, and skips a refresh while one is already in flight. |
| **Reload** | Runs the search again. |
| **Reset** | Clears every filter and runs the search again. |

Above the table is a strip of counts: how many entries each part of the server wrote among everything your
filters match, noisiest first. **They are buttons.** Clicking `Directory watchers (137)` sets the subsystem
filter to `dirwatch` and gives you a log that is only those 137 entries. The counts deliberately ignore the
subsystem filter you already set — a strip narrowed to the one subsystem you chose would offer nothing to
narrow to next — so they always add up to the total of the search you are looking at.

Entries are paged: the view shows ten at a time and asks the server for two hundred at a time.

## Retention

The **Prune days** option in the administration **Options** section is how long the call history and the log
are kept. The server applies it once an hour. `0` switches the retention off, and a deployment with the
retention off writes no retention entry at all.

A pass that ran says what it removed:

```
info prune: the retention pass removed 1204 calls and 318 log entries
```

A pass that could not run says that instead, and says why. Before, a pass wrote a notice *before* it started,
which meant a failed pass looked exactly like a successful one and a pass that had stopped removing anything
was invisible until the disk was full.

## Reading the log when something is wrong

| What you are chasing | Start here |
| --- | --- |
| Recordings are not being picked up | **Directory watchers**, then **Call ingest**. A watch that started names the directory; a watch that stopped or a directory that could not be read names the directory and the failure; a recording that matches no talkgroup says so. |
| A call was recorded but never reached another instance | **Downstreams** — the line names the system, the talkgroup, the destination and the outcome. |
| Calls are being held and not released | **Delayed calls**. |
| Someone cannot sign in, or something is trying | **Logins**. Attempts, refusals and the address they came from. A sign-in that worked writes nothing here; **Administration** is where the app's own connection appears. |
| Clients cannot listen | **Access codes** and **Live feed**. |
| Audio will not play | **Audio conversion**. The entry carries what the converter said, and the reason is at the end of it. |
| A capture box's upload is refused | **Uploads**. The entry names the reason the server gave, which is the text the box gets back. |
| The server restarted, or stopped | **Server**. |
| A system, talkgroup or unit appeared on its own | **Configuration**. Each entry names the recording that caused the server to create it, so the question becomes which recording that was. |
| The administration app itself is doing something odd | **Administration**. The app's connection joining and ending, a session token this server did not issue, a new signing secret — which ends every session that was open before it — and a save whose body the server could not read. |
| You are not sure the history is being trimmed | **Retention**. A pass that ran says how much it removed; a pass that could not run says what stopped it. |
| You are not sure the log itself is working | **Log search** — the log view's own failures are entries too. |
| An upgrade from the previous server went wrong | **Database**: what it migrated, what it refused, and what it left damaged. |

A text search is how you narrow inside a subsystem: `Call ingest` plus the talkgroup number, `Directory
watchers` plus the directory, `Logins` plus the address.
