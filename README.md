# rummy

A small self-hosted web app to track a family's lifetime Rummy scores: add players,
record rounds, and see running standings. Single Go binary, SQLite storage, embedded UI.

## Scoring

Each round has one **declarer** and, for every participating player, two numeric inputs:

- **haath** — points left in the player's haath, paid to the declarer (the declarer's own haath is 0).
- **value** — netted pairwise against every other player in the round.

Each round produces a per-player **delta**, and all deltas in a round sum to 0:

```
haathDelta(declarer)     = + Σ haath(others)
haathDelta(non-declarer) = − haath(self)
valueDelta(i)           = n · value(i) − Σ value(all)     # n = players in that round
roundDelta(i)           = haathDelta(i) + valueDelta(i)
```

Lifetime score = sum of a player's round deltas. **Higher = ahead.** See `DESIGN.md` for the
full derivation and a worked example.

## Run

```sh
nix run .              # or: go run .
```

Then open http://127.0.0.1:8084.

| Env var      | Default       | Meaning                          |
|--------------|---------------|----------------------------------|
| `RUMMY_DIR`  | `.`           | Directory holding `rummy.db`     |
| `RUMMY_HOST` | `127.0.0.1`   | Bind address                     |
| `RUMMY_PORT` | `8084`        | Port                             |

## API

| Method & path             | Body                                                        |
|---------------------------|-------------------------------------------------------------|
| `GET /api/state`          | —                                                           |
| `POST /api/players`       | `{ "name": "Arjun" }`                                        |
| `PUT /api/players/{id}`   | `{ "name": "Arjun G" }`                                      |
| `DELETE /api/players/{id}`| — (409 if the player has played any rounds)                 |
| `POST /api/rounds`        | `{ "declarer_id": 1, "entries": [{player_id,haath,value}] }` |
| `PUT /api/rounds/{id}`    | same shape as POST (full replace)                           |
| `DELETE /api/rounds/{id}` | —                                                           |

## Deploy

`flake.nix` exposes `nixosModules.default` (`services.rummy`). It's wired into the `servers`
repo behind Caddy at `rummy.hospital.monky.cloud`.
