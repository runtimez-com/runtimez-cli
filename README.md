# rtz — the runtimez CLI

Operate and troubleshoot Kubernetes from the runtimez control plane: what is running, what
changed, what is risky, whether a cluster is upgrade-safe, why a workload is slow.

**No kubeconfig, no cluster VPN, no browser.** `rtz` reads everything through the runtimez
API, so it works from a laptop on a phone tether at 3am.

## Install

```bash
brew install runtimez-com/tap/rtz
```

```bash
curl -fsSL https://raw.githubusercontent.com/runtimez-com/runtimez-cli/main/install.sh | sh
```

Windows: `scoop bucket add runtimez https://github.com/runtimez-com/scoop-bucket && scoop install rtz`

Binaries cover macOS, Linux and Windows on amd64 and arm64; `.deb`, `.rpm` and `.apk`
packages are attached to each release. Building from source: `make build`.

See [RELEASING.md](RELEASING.md) for how releases are cut and how to verify a download.

## Quick start

```bash
rtz login                  # browser sign-in
rtz login --token rk_...   # or an API key with the api:read scope, for CI
rtz cluster ls
rtz use prod
rtz doctor
```

## Interactive UI

Run `rtz` with no arguments for the full-screen UI.

| Key | Action |
|---|---|
| `:` | command bar — `:pods`, `:deploy`, `:sts`, `:ds`, `:svc`, `:ing`, `:nodes`, `:all`, `:risk`, `:signals`, `:changes`, `:logs`, `:traces`, `:ns <name>`, `:q` |
| `/` | filter the visible rows as you type |
| `enter` / `d` | describe the selected workload |
| `p` | pause or resume auto-refresh |
| `ctrl+r` | refresh now |
| `?` | keys and commands |
| `q`, `ctrl+c` | quit |

Auto-refresh runs every 10s (`--refresh`) and holds while a pane or prompt is open, so the
rows never move under the cursor. `--view` picks the opening screen.

Every screen has an exact flag equivalent — `:deploy` is `rtz get deploy`, `d` is
`rtz describe` — because both render from the same column definitions in `internal/view`.

On a terminal that cannot host the UI (a pipe, a CI log, `TERM=dumb`) `rtz` prints help
instead. On Windows use Windows Terminal or PowerShell 7; the legacy console lacks the
virtual-terminal support the UI needs.

## Ask the agent

```bash
rtz ask "why is checkout returning 5xx"
rtz ask "and the memory limits?" --session s-1a2b
rtz ask prompts          # curated starter questions
rtz ask sessions         # saved conversations
```

The investigation streams as it runs — each step names the tool it is about to use and what
that tool returned — so a long run shows progress rather than a blank prompt. `ctrl-c`
cancels it **server-side**, not just locally, so an abandoned run stops costing tokens.

`--quiet` prints only the answer. A run that hits the agent's step limit says so: a partial
investigation otherwise reads exactly like a complete one.

## Logs, traces and metrics

```bash
rtz logs --since 15m --level ERROR
rtz logs -s checkout -q 'status:5* AND @http.method:POST'
rtz logs --follow                    # polls; the API has no log stream

rtz trace list --since 2h --errors
rtz trace get <trace-id>             # span tree, nested, errors marked
rtz trace analyze <trace-id>
rtz trace logs <trace-id>            # logs correlated to that trace

rtz metrics list --entity-type K8S_POD
rtz metrics query k8s.pod.memory.usage --entity-type K8S_POD --group-by k8s.pod.name --agg max
rtz metrics tags --entity-type K8S_POD --key namespace
rtz metrics entities --entity-type K8S_NODE
```

`-q` takes the Datadog-style filter language — free text, `field:value`, `@attributes`,
wildcards, ranges, `AND`/`OR`/`NOT`. The server rejects an unparseable query rather than
silently matching everything.

`--entity-type` is required on the metrics endpoints: the API answers a missing one with a
500, so the CLI catches it first and names the flag.

A terminal cannot draw a curve, so `metrics query` summarises each series as last/min/max
plus the point count, and says when the backend rewrote the query as a per-second rate.
Use `-o json` for the raw points.

## Upgrade readiness

```bash
rtz upgrade check                      # is this cluster safe to upgrade?
rtz upgrade check --target 1.31 --fail-on high
rtz upgrade fleet                      # every cluster, soonest deadline first
```

`upgrade check` warns when the verdict rests on **stale** inventory or a **partial** scan, and
names any detection tier that was not fully covered — a readiness score built on old data
otherwise reads exactly like one built on fresh data. Extended-support costs are labelled
`ILLUSTRATIVE` when the backend has not sourced a real figure.

`upgrade fleet` reports clusters it could not analyse rather than folding them into a pass.

## Pre-merge gate

```bash
helm template ./chart | rtz risk check -f - --fail-on high
```

Stateless and cluster-free — it needs only a token, so it runs anywhere CI does. An **empty**
manifest is refused rather than scored 0: a template step that silently produced nothing
would otherwise be the greenest possible build.

## Reliability

Four axes, all answerable without opening the UI.

```bash
rtz risk                            # scored workloads, worst first (0-100, higher is worse)
rtz risk workload payments/checkout # why this one scored what it did
rtz risk security | cve | compliance
rtz rca                             # what is degraded right now, and why
rtz rca payments/checkout           # evidence: restarts, events, log tail, dependencies
rtz rca explain payments/checkout   # narrative root cause
rtz signals                         # slowest operations by p99
rtz signals traces                  # slowest individual traces
rtz changes --since 24              # what changed, newest first
rtz changes payments/checkout       # one workload's history
```

### As a CI gate

```bash
rtz risk --fail-on high
```

Exits `4` when anything reaches the threshold. An unrecognised severity is a usage error,
not a silent pass — a typo must never turn the gate into a no-op that reports success.

### What it tells you when it cannot tell you

These commands distinguish "nothing is wrong" from "we could not see", because a clean score
computed from missing evidence is the most dangerous output this tool could produce:

- `risk` warns when it scored without metrics, CVE data, runtime or network signals.
- `risk security` / `cve` say when a cluster has **never been scanned**, rather than
  reporting zero findings as clean.
- `rca` prints "no trace data for this workload" instead of a fabricated 0% error rate.
- `rca explain` states whether the answer came from a model, a cache replay, or a
  deterministic gate — the prose alone does not reveal which.

## Everyday commands

```bash
rtz get pods -n payments            # po, deploy, sts, ds, svc, ing, no — or "all"
rtz get deploy -o wide              # adds KIND and IMAGE
rtz get pods -l app=web             # equality selectors
rtz describe payments/checkout      # workload + pods + events + services in one screen
rtz search checkout                 # substring match over workload names
rtz ns / rtz counts                 # namespaces, resource counts by kind
rtz fleet                           # org-wide rollup across every cluster
```

`get` reads what the agent last synced, so it needs no kubeconfig and no cluster network
access. The server caps a listing at 5000 rows with no pagination — when that cap is hit,
`rtz` says the list is truncated rather than presenting it as the whole cluster.

## Credentials

Two credential kinds, and they are not interchangeable:

| Kind | How | Reach |
|---|---|---|
| API key (`rk_…`) | `rtz login --token` or `RTZ_TOKEN` | Must carry the **`api:read`** scope. Then authorizes exactly as the user who created it does, for every cluster-scoped command. Carries no role, so org and user administration is out of reach by design. |
| Browser sign-in | `rtz login` | Full role parity, including administration. The redirect returns to a listener on `127.0.0.1` that the command opens; over SSH, `--no-browser` prints the URL and the exact `ssh -L` line to forward the port. |

An **ingest key will not work** — `ingest:*` scopes authorize the ingest gateway, not this
API. Create a separate key with `api:read`.

Credentials go to the OS keychain when one is reachable, and to a `0600` file otherwise —
which is the normal case on a headless Linux box with no D-Bus session.

## Configuration

Contexts work like kubectl's: a named target holding an API URL, an org, and a cluster.

```bash
rtz config get-contexts
rtz config use-context prod
rtz config path
```

Precedence is **flag → environment → context**, so a `--flag` never loses to a config file.

| Flag | Env | Meaning |
|---|---|---|
| `--context` | `RTZ_CONTEXT` | Which stored context to use |
| `--api` | `RTZ_API` | Backend base URL |
| `--org` | `RTZ_ORG` | Organization id |
| `--cluster` | `RTZ_CLUSTER` | Cluster id |
| — | `RTZ_TOKEN` | API key, winning over anything stored |
| — | `RTZ_CONFIG` | Config file path |

## Output

Every command supports `-o table|wide|json|yaml`. The JSON is the unwrapped payload — never
the API envelope — so `jq` needs no `.data` prefix:

```bash
rtz cluster ls -o json | jq -r '.[] | select(.status != "CONNECTED") | .name'
```

## Exit codes

Scripts and CI depend on these.

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Runtime error |
| `2` | Usage error |
| `3` | Authentication required |
| `4` | Policy or threshold failure (`--fail-on`) |

## Development

```bash
make test      # go test ./...
make lint      # golangci-lint
make snapshot  # build every release target locally
```

`internal/api` is the only package that knows HTTP. The flag commands and (later) the TUI are
both consumers of it, which is what keeps the two surfaces from drifting apart.
