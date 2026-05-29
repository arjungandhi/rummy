package main

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

var errPlayerHasEntries = errors.New("player has played rounds")

type Store struct {
	db *sql.DB
}

type Player struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Entry is one player's raw inputs plus the computed delta for a round.
type Entry struct {
	PlayerID int64 `json:"player_id"`
	Hand     int   `json:"hand"`
	Value    int   `json:"value"`
	Delta    int   `json:"delta"`
}

type Round struct {
	ID         int64   `json:"id"`
	DeclarerID int64   `json:"declarer_id"`
	CreatedAt  string  `json:"created_at"`
	Entries    []Entry `json:"entries"`
}

type Standing struct {
	PlayerID int64 `json:"player_id"`
	Score    int   `json:"score"`
	Rounds   int   `json:"rounds"`
}

type State struct {
	Players   []Player   `json:"players"`
	Rounds    []Round    `json:"rounds"`
	Standings []Standing `json:"standings"`
}

// RoundInput is the request shape for creating/updating a round.
type RoundInput struct {
	DeclarerID int64 `json:"declarer_id"`
	Entries    []struct {
		PlayerID int64 `json:"player_id"`
		Hand     int   `json:"hand"`
		Value    int   `json:"value"`
	} `json:"entries"`
}

func NewStore(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS players (
			id   INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		);
		CREATE TABLE IF NOT EXISTS rounds (
			id          INTEGER PRIMARY KEY,
			declarer_id INTEGER NOT NULL REFERENCES players(id),
			created_at  TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS entries (
			id        INTEGER PRIMARY KEY,
			round_id  INTEGER NOT NULL REFERENCES rounds(id) ON DELETE CASCADE,
			player_id INTEGER NOT NULL REFERENCES players(id),
			hand      INTEGER NOT NULL DEFAULT 0,
			value     INTEGER NOT NULL DEFAULT 0,
			UNIQUE(round_id, player_id)
		);
	`); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) AddPlayer(name string) (Player, error) {
	res, err := s.db.Exec(`INSERT INTO players (name) VALUES (?)`, name)
	if err != nil {
		return Player{}, err
	}
	id, _ := res.LastInsertId()
	return Player{ID: id, Name: name}, nil
}

func (s *Store) RenamePlayer(id int64, name string) error {
	res, err := s.db.Exec(`UPDATE players SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeletePlayer(id int64) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM entries WHERE player_id = ?`, id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return errPlayerHasEntries
	}
	_, err := s.db.Exec(`DELETE FROM players WHERE id = ?`, id)
	return err
}

// validateRound checks an incoming round: >=2 distinct participants, declarer among them.
func validateRound(in RoundInput, known map[int64]bool) error {
	if len(in.Entries) < 2 {
		return fmt.Errorf("a round needs at least 2 players")
	}
	seen := map[int64]bool{}
	declarerIncluded := false
	for _, e := range in.Entries {
		if !known[e.PlayerID] {
			return fmt.Errorf("unknown player %d", e.PlayerID)
		}
		if seen[e.PlayerID] {
			return fmt.Errorf("player %d listed twice", e.PlayerID)
		}
		seen[e.PlayerID] = true
		if e.PlayerID == in.DeclarerID {
			declarerIncluded = true
		}
	}
	if !declarerIncluded {
		return fmt.Errorf("declarer must be one of the players in the round")
	}
	return nil
}

func (s *Store) knownPlayers() (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT id FROM players`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		known[id] = true
	}
	return known, rows.Err()
}

// writeEntries validates the input and inserts the round + entries in a transaction.
// If roundID > 0 it updates that round in place (declarer + full entry replace).
func (s *Store) writeRound(roundID int64, in RoundInput) (int64, error) {
	known, err := s.knownPlayers()
	if err != nil {
		return 0, err
	}
	if err := validateRound(in, known); err != nil {
		return 0, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if roundID == 0 {
		res, err := tx.Exec(`INSERT INTO rounds (declarer_id, created_at) VALUES (?, ?)`,
			in.DeclarerID, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return 0, err
		}
		roundID, _ = res.LastInsertId()
	} else {
		res, err := tx.Exec(`UPDATE rounds SET declarer_id = ? WHERE id = ?`, in.DeclarerID, roundID)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return 0, sql.ErrNoRows
		}
		if _, err := tx.Exec(`DELETE FROM entries WHERE round_id = ?`, roundID); err != nil {
			return 0, err
		}
	}

	for _, e := range in.Entries {
		hand := e.Hand
		if e.PlayerID == in.DeclarerID {
			hand = 0 // declarer's hand never counts
		}
		if _, err := tx.Exec(
			`INSERT INTO entries (round_id, player_id, hand, value) VALUES (?, ?, ?, ?)`,
			roundID, e.PlayerID, hand, e.Value,
		); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return roundID, nil
}

func (s *Store) AddRound(in RoundInput) (int64, error) { return s.writeRound(0, in) }
func (s *Store) UpdateRound(id int64, in RoundInput) error {
	_, err := s.writeRound(id, in)
	return err
}

func (s *Store) DeleteRound(id int64) error {
	res, err := s.db.Exec(`DELETE FROM rounds WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// computeDeltas fills Delta on each entry per the scoring rules and returns
// each player's round delta. See DESIGN.md §1.
func computeDeltas(r *Round) map[int64]int {
	n := len(r.Entries)
	sumValue := 0
	sumOtherHands := 0
	for _, e := range r.Entries {
		sumValue += e.Value
		if e.PlayerID != r.DeclarerID {
			sumOtherHands += e.Hand
		}
	}
	deltas := map[int64]int{}
	for i := range r.Entries {
		e := &r.Entries[i]
		valueDelta := n*e.Value - sumValue
		var handDelta int
		if e.PlayerID == r.DeclarerID {
			handDelta = sumOtherHands
		} else {
			handDelta = -e.Hand
		}
		e.Delta = handDelta + valueDelta
		deltas[e.PlayerID] = e.Delta
	}
	return deltas
}

func (s *Store) State() (State, error) {
	st := State{Players: []Player{}, Rounds: []Round{}, Standings: []Standing{}}

	pRows, err := s.db.Query(`SELECT id, name FROM players ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return st, err
	}
	defer pRows.Close()
	for pRows.Next() {
		var p Player
		if err := pRows.Scan(&p.ID, &p.Name); err != nil {
			return st, err
		}
		st.Players = append(st.Players, p)
	}
	if err := pRows.Err(); err != nil {
		return st, err
	}

	rRows, err := s.db.Query(`SELECT id, declarer_id, created_at FROM rounds ORDER BY id`)
	if err != nil {
		return st, err
	}
	defer rRows.Close()
	roundByID := map[int64]*Round{}
	for rRows.Next() {
		var r Round
		r.Entries = []Entry{}
		if err := rRows.Scan(&r.ID, &r.DeclarerID, &r.CreatedAt); err != nil {
			return st, err
		}
		st.Rounds = append(st.Rounds, r)
	}
	if err := rRows.Err(); err != nil {
		return st, err
	}
	for i := range st.Rounds {
		roundByID[st.Rounds[i].ID] = &st.Rounds[i]
	}

	eRows, err := s.db.Query(`SELECT round_id, player_id, hand, value FROM entries ORDER BY id`)
	if err != nil {
		return st, err
	}
	defer eRows.Close()
	for eRows.Next() {
		var rid int64
		var e Entry
		if err := eRows.Scan(&rid, &e.PlayerID, &e.Hand, &e.Value); err != nil {
			return st, err
		}
		if r := roundByID[rid]; r != nil {
			r.Entries = append(r.Entries, e)
		}
	}
	if err := eRows.Err(); err != nil {
		return st, err
	}

	scores := map[int64]int{}
	played := map[int64]int{}
	for i := range st.Rounds {
		deltas := computeDeltas(&st.Rounds[i])
		for pid, d := range deltas {
			scores[pid] += d
			played[pid]++
		}
	}
	for _, p := range st.Players {
		st.Standings = append(st.Standings, Standing{
			PlayerID: p.ID,
			Score:    scores[p.ID],
			Rounds:   played[p.ID],
		})
	}
	// Sort standings by score descending (higher = ahead), then by player id.
	for i := 0; i < len(st.Standings); i++ {
		for j := i + 1; j < len(st.Standings); j++ {
			if st.Standings[j].Score > st.Standings[i].Score ||
				(st.Standings[j].Score == st.Standings[i].Score && st.Standings[j].PlayerID < st.Standings[i].PlayerID) {
				st.Standings[i], st.Standings[j] = st.Standings[j], st.Standings[i]
			}
		}
	}

	return st, nil
}
