# Rummy Scorekeeper — Design

A small web app to track a family's lifetime Rummy scores: add players, record
rounds, and see running standings. Self-hosted on the `servers` box, behind Caddy.

---

## 1. Scoring

Rummy is played in **rounds**. Each round has one **declarer** (the player who went out).
Every player in the round has two numeric inputs:

| Input  | Meaning |
|--------|---------|
| `hand` | Points left in the player's hand. Paid to the declarer. Declarer's own hand is `0`. |
| `value`| A second per-player number that is **netted pairwise** against every other player. |

A round produces a **delta** (score change) for each player. **All deltas in a round sum to 0** —
it is a zero-sum settlement ledger.

### 1.1 Hand component — paid to the declarer

Each non-declarer pays their `hand` to the declarer.

```
handDelta(declarer)     = + Σ hand(j)   for all non-declarers j
handDelta(non-declarer) = − hand(self)
```

The declarer's own `hand` is always `0` (they went out), so it contributes nothing.
This component sums to 0 on its own.

### 1.2 Value component — pairwise net across all players

Against each *other* player, you net the difference of your `value`. Summing over all
opponents collapses to a closed form. **n = number of players who played in *that* round
(declarer included).** Not every player on the roster plays every round, so `n` is the count
of participants in the specific round, not the size of the roster.

```
valueDelta(i) = n · value(i) − Σ value(all players)
```

Derivation: `Σⱼ≠ᵢ (value(i) − value(j)) = (n−1)·value(i) − (Σvalue − value(i)) = n·value(i) − Σvalue`.

Every player (including the declarer) participates in value netting. This component also
sums to 0 on its own.

### 1.3 Round delta and lifetime score

```
roundDelta(i)   = handDelta(i) + valueDelta(i)
lifetimeScore(i)= Σ roundDelta(i)   over all rounds the player appears in
```

Because each component is zero-sum, every round sums to 0 and so does the lifetime board.
**Higher score = ahead ("up"); lower = behind ("down").**

### 1.4 Worked example (4 players, p1 declares)

`Σvalue = 3+4+1+6 = 14`, `n = 4`. Value part = `4·value − 14`. Hand part for p1 = `5+8+2`.

| player        | hand | value | hand part | value part | **round delta** |
|---------------|------|-------|-----------|-----------|-----------------|
| p1 (declarer) | 0    | 3     | +15       | −2        | **+13**         |
| p2            | 5    | 4     | −5        | +2        | **−3**          |
| p3            | 8    | 1     | −8        | −10       | **−18**         |
| p4            | 2    | 6     | −2        | +10       | **+8**          |
| **sum**       |      |       | 0         | 0         | **0**           |

---

## 2. Software Architecture

### 2.1 Stack & conventions (matches sibling repos)

- **Go** single binary, built with `pkgs.buildGoModule` + `flake-utils` (like `metrics`).
- **SQLite** via `modernc.org/sqlite` — pure-Go driver, no CGO, builds cleanly under Nix.
- **Frontend**: one static `index.html` (plain HTML + a little vanilla JS), `go:embed`-ed
  into the binary so deployment is a single artifact. No build step, no framework.
- Zero runtime deps beyond the binary and its SQLite file.

### 2.2 Process / layout

```
rummy/
  main.go          # HTTP server: serves UI + JSON API, opens SQLite
  store.go         # DB schema, queries, score computation
  index.html       # embedded UI
  go.mod / go.sum
  flake.nix        # packages.default (binary) + nixosModules.default (systemd service)
  DESIGN.md
```

The server listens on `127.0.0.1:$RUMMY_PORT` (default `8084`) and opens the SQLite DB at
`$RUMMY_DIR/rummy.db`. Caddy reverse-proxies `rummy.hospital.monky.cloud` to it (wired up later).

### 2.3 Data model (SQLite)

```sql
players(
  id    INTEGER PRIMARY KEY,
  name  TEXT NOT NULL UNIQUE
);

rounds(
  id          INTEGER PRIMARY KEY,
  declarer_id INTEGER NOT NULL REFERENCES players(id),
  created_at  TEXT NOT NULL            -- ISO timestamp
);

entries(
  id        INTEGER PRIMARY KEY,
  round_id  INTEGER NOT NULL REFERENCES rounds(id) ON DELETE CASCADE,
  player_id INTEGER NOT NULL REFERENCES players(id),
  hand      INTEGER NOT NULL DEFAULT 0,
  value     INTEGER NOT NULL DEFAULT 0,
  UNIQUE(round_id, player_id)
);
```

**A round contains one `entries` row per *participating* player only.** Players who sit a round
out simply have no entry, so `n` (§1.2) is `COUNT(entries)` for that round. The declarer must be
among the participants.

No stored `number`: rounds are displayed in `created_at`/`id` order, so deleting a round leaves
no gaps to renumber. Deleting a round cascades to its entries.

Stored data is just the **raw inputs** (hand, value, who declared, who participated). All deltas
and standings are **computed on read** in Go from §1 — never stored — so the rules live in exactly
one place and historical rounds recompute correctly if logic is ever tweaked.

### 2.4 HTTP API

| Method & path             | Body                                                        | Returns |
|---------------------------|-------------------------------------------------------------|---------|
| `GET /`                   | —                                                           | embedded `index.html` |
| `GET /api/state`          | —                                                           | `{ players, rounds[], standings[] }` with computed deltas |
| `POST /api/players`       | `{ "name": "Arjun" }`                                        | created player |
| `PUT /api/players/{id}`   | `{ "name": "Arjun G" }`                                      | renamed player |
| `DELETE /api/players/{id}`| —                                                           | `204`; rejected `409` if the player has any entries |
| `POST /api/rounds`        | `{ "declarer_id": 1, "entries": [{player_id,hand,value}] }` | created round + computed deltas |
| `PUT /api/rounds/{id}`    | same shape as `POST /api/rounds`                            | updated round (replaces declarer + all entries) |
| `DELETE /api/rounds/{id}` | —                                                           | `204`; cascades to entries |

`GET /api/state` is the single source the UI renders from. Each round in the response
includes per-player `hand`, `value`, and computed `delta`; `standings` is the lifetime board
sorted by score descending.

Editing a round (`PUT`) is a full replace: validate the new participant set (declarer included,
≥2 players, declarer's hand forced to 0), delete the old entries, and insert the new ones in one
transaction. Player delete is only allowed for a player who never played, to keep historical
round math intact; renaming is fine.

### 2.5 UI (single page)

Three sections on one page, all driven by `/api/state`:

1. **Standings** — table of players sorted by lifetime score (high → low).
2. **Players** — name field + button to add; rename inline; delete (disabled/hidden once a
   player has played a round).
3. **Add round** — first tick which players are *in* this round, pick the declarer from them,
   then a row per participant with `hand` and `value` inputs (declarer's `hand` locked to 0);
   submit.
4. **History** — past rounds (newest first) showing participants, inputs, and computed deltas,
   each with **Edit** (reopens the round form prefilled) and **Delete** controls.

### 2.6 Deployment (later)

Mirrors `homiefans`/`tobor`: `rummy` becomes a flake input in `servers/flake.nix`, its
`nixosModules.default` adds a systemd service on port `8084` with `RUMMY_DIR` pointed at synced
storage, and a Caddy virtualHost (`rummy.hospital.monky.cloud`) reverse-proxies to it **behind
Authelia forward-auth** (same `forward_auth` block the other protected services use).

---

## 3. Open questions

None outstanding — design is locked for v1.
