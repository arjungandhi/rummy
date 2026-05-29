package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed index.html
var assets embed.FS

type Server struct {
	store *Store
}

func main() {
	dir := envOr("RUMMY_DIR", ".")
	host := envOr("RUMMY_HOST", "127.0.0.1")
	port := envOr("RUMMY_PORT", "8084")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("creating data dir: %v", err)
	}
	dbPath := filepath.Join(dir, "rummy.db")
	store, err := NewStore(dbPath)
	if err != nil {
		log.Fatalf("opening db: %v", err)
	}
	srv := &Server{store: store}

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleRoot)
	mux.HandleFunc("/api/state", srv.handleState)
	mux.HandleFunc("/api/players", srv.handlePlayers)
	mux.HandleFunc("/api/players/", srv.handlePlayerByID)
	mux.HandleFunc("/api/rounds", srv.handleRounds)
	mux.HandleFunc("/api/rounds/", srv.handleRoundByID)

	addr := host + ":" + port
	log.Printf("rummy serving on http://%s (db: %s)", addr, dbPath)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	body, _ := assets.ReadFile("index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(body)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	st, err := s.store.State()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handlePlayers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	p, err := s.store.AddPlayer(name)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "a player with that name already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handlePlayerByID(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(r.URL.Path, "/api/players/")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad player id")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body struct {
			Name string `json:"name"`
		}
		if err := decode(r, &body); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			writeErr(w, http.StatusBadRequest, "name required")
			return
		}
		if err := s.store.RenamePlayer(id, name); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeErr(w, http.StatusNotFound, "player not found")
				return
			}
			if strings.Contains(err.Error(), "UNIQUE") {
				writeErr(w, http.StatusConflict, "a player with that name already exists")
				return
			}
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := s.store.DeletePlayer(id); err != nil {
			if errors.Is(err, errPlayerHasEntries) {
				writeErr(w, http.StatusConflict, "can't delete a player who has played rounds")
				return
			}
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleRounds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var in RoundInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.store.AddRound(in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

func (s *Server) handleRoundByID(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(r.URL.Path, "/api/rounds/")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad round id")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var in RoundInput
		if err := decode(r, &in); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.store.UpdateRound(id, in); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeErr(w, http.StatusNotFound, "round not found")
				return
			}
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := s.store.DeleteRound(id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeErr(w, http.StatusNotFound, "round not found")
				return
			}
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func idFromPath(path, prefix string) (int64, bool) {
	raw := strings.TrimPrefix(path, prefix)
	raw = strings.Trim(raw, "/")
	if raw == "" || strings.Contains(raw, "/") {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
