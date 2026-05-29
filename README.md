# rummy

A small self-hosted web app to track a family's lifetime Rummy scores: add players,
record rounds, and see running standings. Single Go binary, SQLite storage, embedded UI.

## Scoring

Each round has one **declarer** and, for every participating player, two numeric inputs:

- **hand** — points left in the player's hand, paid to the declarer (the declarer's own hand is 0).
- **value** — netted pairwise against every other player in the round.

Each round produces a per-player **delta**, and all deltas in a round sum to 0:

```
handDelta(declarer)     = + Σ hand(others)
handDelta(non-declarer) = − hand(self)
valueDelta(i)           = n · value(i) − Σ value(all)     # n = players in that round
roundDelta(i)           = handDelta(i) + valueDelta(i)
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
| `POST /api/rounds`        | `{ "declarer_id": 1, "entries": [{player_id,hand,value}] }` |
| `PUT /api/rounds/{id}`    | same shape as POST (full replace)                           |
| `DELETE /api/rounds/{id}` | —                                                           |

## Deploy

`flake.nix` exposes `nixosModules.default` (`services.rummy`). It's wired into the `servers`
repo behind Caddy at `rummy.hospital.monky.cloud`.
