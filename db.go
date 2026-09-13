package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("a bin with that code already exists")
)

// InputError is a problem with what the user entered; shown to them as-is.
type InputError struct{ msg string }

func (e InputError) Error() string { return e.msg }

func invalid(format string, args ...any) error { return InputError{fmt.Sprintf(format, args...)} }

const schema = `
CREATE TABLE IF NOT EXISTS bins (
	id         INTEGER PRIMARY KEY,
	code       TEXT NOT NULL UNIQUE COLLATE NOCASE,
	name       TEXT NOT NULL DEFAULT '',
	location   TEXT NOT NULL DEFAULT '',
	notes      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS items (
	id         INTEGER PRIMARY KEY,
	bin_id     INTEGER NOT NULL REFERENCES bins(id) ON DELETE CASCADE,
	name       TEXT NOT NULL,
	quantity   INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS items_bin ON items(bin_id);
CREATE TABLE IF NOT EXISTS history (
	id         INTEGER PRIMARY KEY,
	bin_code   TEXT NOT NULL,
	action     TEXT NOT NULL,
	details    TEXT NOT NULL DEFAULT '',
	actor      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS history_bin ON history(bin_code COLLATE NOCASE, id);
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

type Bin struct {
	ID        int64  `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Location  string `json:"location"`
	Notes     string `json:"notes"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	ItemCount int    `json:"itemCount"`
	TotalQty  int    `json:"totalQty"`
	// omitzero (not omitempty): a bin with no items must still send "items": [];
	// only bin lists, which never load items, leave the field out.
	Items        []Item   `json:"items,omitzero"`
	MatchedItems []string `json:"matchedItems,omitempty"`
}

type Item struct {
	ID        int64  `json:"id"`
	BinID     int64  `json:"binId"`
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	UpdatedAt string `json:"updatedAt"`
}

type HistoryEntry struct {
	ID        int64  `json:"id"`
	BinCode   string `json:"binCode"`
	Action    string `json:"action"`
	Details   string `json:"details"`
	Actor     string `json:"actor"`
	CreatedAt string `json:"createdAt"`
}

type BinPatch struct {
	Name     *string `json:"name"`
	Location *string `json:"location"`
	Notes    *string `json:"notes"`
}

type ItemPatch struct {
	Name     *string `json:"name"`
	Quantity *int    `json:"quantity"`
}

// querier is satisfied by both *sql.DB and *sql.Tx.
type querier interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Store wraps the SQLite database. Normal reads and writes take the read
// lock (SQLite serializes them itself); restoring a backup takes the write
// lock so nothing touches the file while it is swapped.
type Store struct {
	mu        sync.RWMutex
	db        *sql.DB
	path      string
	backupDir string
}

func OpenStore(path, backupDir string) (*Store, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, path: path, backupDir: backupDir}, nil
}

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	// One connection keeps writes strictly ordered; traffic is tiny.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		s.db.Close()
		s.db = nil
	}
}

func (s *Store) write(fn func(tx *sql.Tx) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// ---- input cleanup ----

func cleanText(s string, max int) string {
	s = strings.TrimSpace(strings.ToValidUTF8(s, ""))
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max])
	}
	return s
}

// normalizeCode upper-cases and trims a scanned or typed bin code. Codes are
// limited to characters that print as any barcode and are safe in URLs.
func normalizeCode(code string) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return "", invalid("Bin code is empty.")
	}
	if len(code) > 40 {
		return "", invalid("Bin code is too long (40 characters max).")
	}
	if !validCodeChars(code) {
		return "", invalid("Bin codes can only use letters, numbers, and - _ . : +")
	}
	return code, nil
}

func validCodeChars(s string) bool {
	for _, r := range s {
		ok := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || strings.ContainsRune("-_.:+", r)
		if !ok {
			return false
		}
	}
	return true
}

func cleanActor(actor string) string {
	actor = cleanText(actor, 40)
	if actor == "" {
		return "Unnamed device"
	}
	return actor
}

func likePattern(term string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(term) + "%"
}

// ---- settings ----

func getSetting(q querier, key, def string) string {
	var v string
	if err := q.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err != nil {
		return def
	}
	return v
}

func setSetting(q querier, key, value string) error {
	_, err := q.Exec(`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) GetSetting(key, def string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return getSetting(s.db, key, def)
}

func (s *Store) SetSetting(key, value string) error {
	return s.write(func(tx *sql.Tx) error { return setSetting(tx, key, value) })
}

func (s *Store) GetSettingInt(key string, def int) int {
	n, err := strconv.Atoi(s.GetSetting(key, strconv.Itoa(def)))
	if err != nil {
		return def
	}
	return n
}

// ---- history ----

func addHistory(q querier, code, action, details, actor string) error {
	_, err := q.Exec(`INSERT INTO history(bin_code, action, details, actor, created_at) VALUES(?, ?, ?, ?, ?)`,
		code, action, details, cleanActor(actor), now())
	return err
}

// History returns newest-first entries, optionally for one bin or matching
// a search term. beforeID pages backwards through older entries.
func (s *Store) History(binCode, search string, limit int, beforeID int64) ([]HistoryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	where := []string{"1=1"}
	var args []any
	if binCode != "" {
		where = append(where, "bin_code = ? COLLATE NOCASE")
		args = append(args, binCode)
	}
	if beforeID > 0 {
		where = append(where, "id < ?")
		args = append(args, beforeID)
	}
	for _, t := range strings.Fields(search) {
		p := likePattern(t)
		where = append(where, `(bin_code LIKE ? ESCAPE '\' OR details LIKE ? ESCAPE '\' OR actor LIKE ? ESCAPE '\' OR action LIKE ? ESCAPE '\')`)
		args = append(args, p, p, p, p)
	}
	args = append(args, limit)
	rows, err := s.db.Query(`SELECT id, bin_code, action, details, actor, created_at FROM history WHERE `+
		strings.Join(where, " AND ")+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HistoryEntry{}
	for rows.Next() {
		var h HistoryEntry
		if err := rows.Scan(&h.ID, &h.BinCode, &h.Action, &h.Details, &h.Actor, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ---- codes ----

// nextCodes hands out n unused codes like BIN-00001 and advances the counter,
// so pre-printed labels never collide with later bins.
func nextCodes(q querier, n int) ([]string, error) {
	prefix := getSetting(q, "code_prefix", "BIN-")
	next, err := strconv.Atoi(getSetting(q, "next_number", "1"))
	if err != nil || next < 1 {
		next = 1
	}
	var codes []string
	for len(codes) < n {
		candidate := fmt.Sprintf("%s%05d", prefix, next)
		next++
		var exists int
		if err := q.QueryRow(`SELECT COUNT(*) FROM bins WHERE code = ?`, candidate).Scan(&exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			codes = append(codes, candidate)
		}
	}
	return codes, setSetting(q, "next_number", strconv.Itoa(next))
}

func (s *Store) ReserveCodes(n int, actor string) ([]string, error) {
	if n < 1 || n > 1000 {
		return nil, invalid("Choose between 1 and 1000 labels.")
	}
	var codes []string
	err := s.write(func(tx *sql.Tx) error {
		var err error
		codes, err = nextCodes(tx, n)
		return err
	})
	return codes, err
}

// ---- bins ----

const binColumns = `b.id, b.code, b.name, b.location, b.notes, b.created_at, b.updated_at,
	(SELECT COUNT(*) FROM items i WHERE i.bin_id = b.id),
	(SELECT COALESCE(SUM(quantity), 0) FROM items i WHERE i.bin_id = b.id)`

func scanBin(row interface{ Scan(...any) error }) (*Bin, error) {
	var b Bin
	err := row.Scan(&b.ID, &b.Code, &b.Name, &b.Location, &b.Notes, &b.CreatedAt, &b.UpdatedAt, &b.ItemCount, &b.TotalQty)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &b, err
}

func getBin(q querier, code string) (*Bin, error) {
	return scanBin(q.QueryRow(`SELECT `+binColumns+` FROM bins b WHERE b.code = ?`, code))
}

func loadItems(q querier, binID int64) ([]Item, error) {
	rows, err := q.Query(`SELECT id, bin_id, name, quantity, updated_at FROM items WHERE bin_id = ? ORDER BY name COLLATE NOCASE`, binID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Item{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.BinID, &it.Name, &it.Quantity, &it.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// ListBins returns bins matching every word of query (in the code, name,
// location, notes, or any item name), optionally limited to one location.
func (s *Store) ListBins(query, location string) ([]Bin, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var where []string
	var args []any
	if location != "" {
		where = append(where, "b.location = ? COLLATE NOCASE")
		args = append(args, location)
	}
	terms := strings.Fields(query)
	for _, t := range terms {
		p := likePattern(t)
		where = append(where, `(b.code LIKE ? ESCAPE '\' OR b.name LIKE ? ESCAPE '\' OR b.location LIKE ? ESCAPE '\' OR b.notes LIKE ? ESCAPE '\'
			OR EXISTS (SELECT 1 FROM items x WHERE x.bin_id = b.id AND x.name LIKE ? ESCAPE '\'))`)
		args = append(args, p, p, p, p, p)
	}
	q := `SELECT ` + binColumns + ` FROM bins b`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY b.code COLLATE NOCASE"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	bins := []Bin{}
	index := map[int64]int{}
	for rows.Next() {
		b, err := scanBin(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		index[b.ID] = len(bins)
		bins = append(bins, *b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Show which items matched so "where are my zip ties?" is answered at a glance.
	if len(terms) > 0 && len(bins) > 0 {
		var conds []string
		var itemArgs []any
		for _, t := range terms {
			conds = append(conds, `name LIKE ? ESCAPE '\'`)
			itemArgs = append(itemArgs, likePattern(t))
		}
		rows, err := s.db.Query(`SELECT bin_id, name, quantity FROM items WHERE `+strings.Join(conds, " OR ")+` ORDER BY name COLLATE NOCASE`, itemArgs...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var binID int64
			var name string
			var qty int
			if err := rows.Scan(&binID, &name, &qty); err != nil {
				return nil, err
			}
			if i, ok := index[binID]; ok {
				bins[i].MatchedItems = append(bins[i].MatchedItems, fmt.Sprintf("%s (%d)", name, qty))
			}
		}
	}
	return bins, nil
}

func (s *Store) GetBin(code string) (*Bin, error) {
	code, err := normalizeCode(code)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, err := getBin(s.db, code)
	if err != nil {
		return nil, err
	}
	b.Items, err = loadItems(s.db, b.ID)
	return b, err
}

type BinInput struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Notes    string `json:"notes"`
}

func (s *Store) CreateBin(in BinInput, actor string) (*Bin, error) {
	name := cleanText(in.Name, 200)
	location := cleanText(in.Location, 200)
	notes := cleanText(in.Notes, 5000)
	var code string
	if strings.TrimSpace(in.Code) != "" {
		var err error
		if code, err = normalizeCode(in.Code); err != nil {
			return nil, err
		}
	}

	err := s.write(func(tx *sql.Tx) error {
		if code == "" {
			codes, err := nextCodes(tx, 1)
			if err != nil {
				return err
			}
			code = codes[0]
		} else if _, err := getBin(tx, code); err == nil {
			return ErrExists
		}
		ts := now()
		if _, err := tx.Exec(`INSERT INTO bins(code, name, location, notes, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`,
			code, name, location, notes, ts, ts); err != nil {
			return err
		}
		details := describe("Name", name) + describe("Location", location)
		return addHistory(tx, code, "Created bin", strings.TrimSuffix(details, "; "), actor)
	})
	if err != nil {
		return nil, err
	}
	return s.GetBin(code)
}

func describe(label, value string) string {
	if value == "" {
		return ""
	}
	return label + ": " + value + "; "
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func (s *Store) UpdateBin(code string, p BinPatch, actor string) (*Bin, error) {
	code, err := normalizeCode(code)
	if err != nil {
		return nil, err
	}
	err = s.write(func(tx *sql.Tx) error {
		b, err := getBin(tx, code)
		if err != nil {
			return err
		}
		name, location, notes := b.Name, b.Location, b.Notes
		if p.Name != nil {
			name = cleanText(*p.Name, 200)
		}
		if p.Location != nil {
			location = cleanText(*p.Location, 200)
		}
		if p.Notes != nil {
			notes = cleanText(*p.Notes, 5000)
		}
		if name == b.Name && location == b.Location && notes == b.Notes {
			return nil
		}
		if _, err := tx.Exec(`UPDATE bins SET name = ?, location = ?, notes = ?, updated_at = ? WHERE id = ?`,
			name, location, notes, now(), b.ID); err != nil {
			return err
		}
		if name != b.Name {
			if err := addHistory(tx, code, "Renamed", orNone(b.Name)+" → "+orNone(name), actor); err != nil {
				return err
			}
		}
		if location != b.Location {
			if err := addHistory(tx, code, "Moved", orNone(b.Location)+" → "+orNone(location), actor); err != nil {
				return err
			}
		}
		if notes != b.Notes {
			if err := addHistory(tx, code, "Notes changed", "", actor); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetBin(code)
}

func (s *Store) DeleteBin(code, actor string) error {
	code, err := normalizeCode(code)
	if err != nil {
		return err
	}
	return s.write(func(tx *sql.Tx) error {
		b, err := getBin(tx, code)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM bins WHERE id = ?`, b.ID); err != nil {
			return err
		}
		details := fmt.Sprintf("%s; Location: %s; %d item types, %d total", orNone(b.Name), orNone(b.Location), b.ItemCount, b.TotalQty)
		return addHistory(tx, code, "Deleted bin", details, actor)
	})
}

func (s *Store) Locations() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(`SELECT MIN(location) FROM bins WHERE location <> '' GROUP BY location COLLATE NOCASE ORDER BY location COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

type Stats struct {
	Bins      int `json:"bins"`
	ItemTypes int `json:"itemTypes"`
	TotalQty  int `json:"totalQty"`
	Locations int `json:"locations"`
}

func (s *Store) Stats() (Stats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var st Stats
	err := s.db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM bins),
		(SELECT COUNT(*) FROM items),
		(SELECT COALESCE(SUM(quantity), 0) FROM items),
		(SELECT COUNT(DISTINCT location COLLATE NOCASE) FROM bins WHERE location <> '')`).
		Scan(&st.Bins, &st.ItemTypes, &st.TotalQty, &st.Locations)
	return st, err
}

// ---- items ----

// AddItem adds quantity to a bin. If an item with the same name is already
// in the bin, its quantity is increased instead of creating a duplicate.
func (s *Store) AddItem(code, name string, qty int, actor string) (*Bin, error) {
	code, err := normalizeCode(code)
	if err != nil {
		return nil, err
	}
	name = cleanText(name, 200)
	if name == "" {
		return nil, invalid("Item name is empty.")
	}
	if qty < 1 || qty > 1_000_000_000 {
		return nil, invalid("Quantity must be at least 1.")
	}
	err = s.write(func(tx *sql.Tx) error {
		b, err := getBin(tx, code)
		if err != nil {
			return err
		}
		ts := now()
		var id int64
		var existing int
		err = tx.QueryRow(`SELECT id, quantity FROM items WHERE bin_id = ? AND name = ? COLLATE NOCASE`, b.ID, name).Scan(&id, &existing)
		switch {
		case err == nil:
			if _, err := tx.Exec(`UPDATE items SET quantity = ?, updated_at = ? WHERE id = ?`, existing+qty, ts, id); err != nil {
				return err
			}
			return addHistory(tx, code, "Added items", fmt.Sprintf("+%d %s (now %d)", qty, name, existing+qty), actor)
		case errors.Is(err, sql.ErrNoRows):
			if _, err := tx.Exec(`INSERT INTO items(bin_id, name, quantity, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
				b.ID, name, qty, ts, ts); err != nil {
				return err
			}
			return addHistory(tx, code, "Added items", fmt.Sprintf("+%d %s", qty, name), actor)
		default:
			return err
		}
	})
	if err != nil {
		return nil, err
	}
	return s.GetBin(code)
}

func itemWithBin(q querier, id int64) (Item, string, error) {
	var it Item
	var code string
	err := q.QueryRow(`SELECT i.id, i.bin_id, i.name, i.quantity, i.updated_at, b.code FROM items i JOIN bins b ON b.id = i.bin_id WHERE i.id = ?`, id).
		Scan(&it.ID, &it.BinID, &it.Name, &it.Quantity, &it.UpdatedAt, &code)
	if errors.Is(err, sql.ErrNoRows) {
		return it, "", ErrNotFound
	}
	return it, code, err
}

func (s *Store) UpdateItem(id int64, p ItemPatch, actor string) (*Bin, error) {
	var code string
	err := s.write(func(tx *sql.Tx) error {
		it, c, err := itemWithBin(tx, id)
		if err != nil {
			return err
		}
		code = c
		name, qty := it.Name, it.Quantity
		if p.Name != nil {
			if name = cleanText(*p.Name, 200); name == "" {
				return invalid("Item name is empty.")
			}
		}
		if p.Quantity != nil {
			if qty = *p.Quantity; qty < 0 || qty > 1_000_000_000 {
				return invalid("Quantity can't be negative.")
			}
		}
		if name == it.Name && qty == it.Quantity {
			return nil
		}
		if name != it.Name {
			var clash int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM items WHERE bin_id = ? AND name = ? COLLATE NOCASE AND id <> ?`, it.BinID, name, id).Scan(&clash); err != nil {
				return err
			}
			if clash > 0 {
				return invalid("This bin already has an item called %q.", name)
			}
		}
		if _, err := tx.Exec(`UPDATE items SET name = ?, quantity = ?, updated_at = ? WHERE id = ?`, name, qty, now(), id); err != nil {
			return err
		}
		if name != it.Name {
			if err := addHistory(tx, code, "Renamed item", it.Name+" → "+name, actor); err != nil {
				return err
			}
		}
		if qty != it.Quantity {
			return addHistory(tx, code, "Changed quantity", fmt.Sprintf("%s: %d → %d", name, it.Quantity, qty), actor)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetBin(code)
}

func (s *Store) DeleteItem(id int64, actor string) (*Bin, error) {
	var code string
	err := s.write(func(tx *sql.Tx) error {
		it, c, err := itemWithBin(tx, id)
		if err != nil {
			return err
		}
		code = c
		if _, err := tx.Exec(`DELETE FROM items WHERE id = ?`, id); err != nil {
			return err
		}
		return addHistory(tx, code, "Removed item", fmt.Sprintf("%s (had %d)", it.Name, it.Quantity), actor)
	})
	if err != nil {
		return nil, err
	}
	return s.GetBin(code)
}
