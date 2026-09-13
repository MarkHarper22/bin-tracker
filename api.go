package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

var quitOnce sync.Once

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"app": "bintracker", "version": appVersion})
	})
	mux.HandleFunc("GET /api/info", a.handleInfo)
	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		st, err := a.store.Stats()
		respond(w, st, err)
	})

	mux.HandleFunc("GET /api/bins", func(w http.ResponseWriter, r *http.Request) {
		bins, err := a.store.ListBins(r.URL.Query().Get("q"), r.URL.Query().Get("location"))
		respond(w, bins, err)
	})
	mux.HandleFunc("POST /api/bins", func(w http.ResponseWriter, r *http.Request) {
		var in BinInput
		if !readJSON(w, r, &in) {
			return
		}
		bin, err := a.store.CreateBin(in, actor(r))
		respond(w, bin, err)
	})
	mux.HandleFunc("GET /api/bins/{code}", func(w http.ResponseWriter, r *http.Request) {
		bin, err := a.store.GetBin(r.PathValue("code"))
		respond(w, bin, err)
	})
	mux.HandleFunc("PATCH /api/bins/{code}", func(w http.ResponseWriter, r *http.Request) {
		var p BinPatch
		if !readJSON(w, r, &p) {
			return
		}
		bin, err := a.store.UpdateBin(r.PathValue("code"), p, actor(r))
		respond(w, bin, err)
	})
	mux.HandleFunc("DELETE /api/bins/{code}", func(w http.ResponseWriter, r *http.Request) {
		err := a.store.DeleteBin(r.PathValue("code"), actor(r))
		respond(w, map[string]bool{"ok": true}, err)
	})

	mux.HandleFunc("POST /api/bins/{code}/items", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name     string `json:"name"`
			Quantity int    `json:"quantity"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		bin, err := a.store.AddItem(r.PathValue("code"), in.Name, in.Quantity, actor(r))
		respond(w, bin, err)
	})
	mux.HandleFunc("PATCH /api/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			respond(w, nil, ErrNotFound)
			return
		}
		var p ItemPatch
		if !readJSON(w, r, &p) {
			return
		}
		bin, err := a.store.UpdateItem(id, p, actor(r))
		respond(w, bin, err)
	})
	mux.HandleFunc("DELETE /api/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			respond(w, nil, ErrNotFound)
			return
		}
		bin, err := a.store.DeleteItem(id, actor(r))
		respond(w, bin, err)
	})

	mux.HandleFunc("GET /api/locations", func(w http.ResponseWriter, r *http.Request) {
		locs, err := a.store.Locations()
		respond(w, locs, err)
	})
	mux.HandleFunc("GET /api/history", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
		entries, err := a.store.History(q.Get("bin"), q.Get("q"), limit, before)
		respond(w, entries, err)
	})

	mux.HandleFunc("POST /api/codes/reserve", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Count int `json:"count"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		codes, err := a.store.ReserveCodes(in.Count, actor(r))
		respond(w, codes, err)
	})
	mux.HandleFunc("GET /api/code.svg", func(w http.ResponseWriter, r *http.Request) {
		svg, err := renderSVG(r.URL.Query().Get("type"), r.URL.Query().Get("data"))
		if err != nil {
			respond(w, nil, err)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(svg)
	})
	mux.HandleFunc("POST /api/decode", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 15<<20)
		text, format, err := decodeImage(r.Body)
		if errors.Is(err, ErrNoCode) {
			writeJSON(w, map[string]any{"found": false})
			return
		}
		respond(w, map[string]any{"found": true, "text": text, "format": format}, err)
	})

	mux.HandleFunc("GET /api/settings", a.handleGetSettings)
	mux.HandleFunc("PATCH /api/settings", a.handlePatchSettings)

	mux.HandleFunc("GET /api/backups", func(w http.ResponseWriter, r *http.Request) {
		list, err := a.store.ListBackups()
		respond(w, list, err)
	})
	mux.HandleFunc("POST /api/backups", func(w http.ResponseWriter, r *http.Request) {
		info, err := a.store.BackupNow("manual")
		respond(w, info, err)
	})
	mux.HandleFunc("POST /api/backups/{name}/restore", func(w http.ResponseWriter, r *http.Request) {
		err := a.store.RestoreBackup(r.PathValue("name"), actor(r))
		respond(w, map[string]bool{"ok": true}, err)
	})

	mux.HandleFunc("POST /api/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if a.serverMode {
			writeError(w, http.StatusForbidden, "Bin Tracker is running as a server. Stop it on the server instead, e.g. with docker stop.")
			return
		}
		if !isLocalRequest(r) {
			writeError(w, http.StatusForbidden, "Bin Tracker can only be stopped from the computer it runs on.")
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
		quitOnce.Do(func() { close(a.quit) })
	})

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Unknown API endpoint.")
	})

	static, _ := fs.Sub(webFiles, "web")
	mux.Handle("/", http.FileServerFS(static))

	return withHeaders(mux)
}

// withHeaders adds safety headers and blocks cross-site writes: every change
// must carry a custom header, which other websites can't send to this app.
func withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Method != http.MethodGet && r.Header.Get("X-BinTracker") != "1" {
				writeError(w, http.StatusForbidden, "Missing request header.")
				return
			}
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"version":    appVersion,
		"localUrl":   fmt.Sprintf("http://localhost:%d", a.httpPort),
		"phoneUrls":  a.phoneAddresses(),
		"httpsPort":  a.httpsPort,
		"serverMode": a.serverMode,
		"dataDir":    a.dataDir,
		"isLocal":    !a.serverMode && isLocalRequest(r),
	})
}

type settingsView struct {
	CodePrefix  string `json:"codePrefix"`
	NextNumber  int    `json:"nextNumber"`
	BackupHours int    `json:"backupHours"`
	BackupKeep  int    `json:"backupKeep"`
	LastBackup  string `json:"lastBackup"`
}

func (a *App) currentSettings() settingsView {
	return settingsView{
		CodePrefix:  a.store.GetSetting("code_prefix", "BIN-"),
		NextNumber:  a.store.GetSettingInt("next_number", 1),
		BackupHours: a.store.GetSettingInt("backup_hours", 24),
		BackupKeep:  a.store.GetSettingInt("backup_keep", 14),
		LastBackup:  a.store.GetSetting("last_backup", ""),
	}
}

func (a *App) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, a.currentSettings())
}

func (a *App) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CodePrefix  *string `json:"codePrefix"`
		BackupHours *int    `json:"backupHours"`
		BackupKeep  *int    `json:"backupKeep"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.CodePrefix != nil {
		p := strings.ToUpper(strings.TrimSpace(*in.CodePrefix))
		if len(p) > 12 || !validCodeChars(p) {
			respond(w, nil, invalid("Code prefix can be up to 12 letters, numbers, or - _ . : +"))
			return
		}
		if err := a.store.SetSetting("code_prefix", p); err != nil {
			respond(w, nil, err)
			return
		}
	}
	if in.BackupHours != nil {
		if *in.BackupHours < 0 || *in.BackupHours > 24*30 {
			respond(w, nil, invalid("Backup interval must be between 0 (off) and 720 hours."))
			return
		}
		if err := a.store.SetSetting("backup_hours", strconv.Itoa(*in.BackupHours)); err != nil {
			respond(w, nil, err)
			return
		}
	}
	if in.BackupKeep != nil {
		if *in.BackupKeep < 1 || *in.BackupKeep > 365 {
			respond(w, nil, invalid("Keep between 1 and 365 automatic backups."))
			return
		}
		if err := a.store.SetSetting("backup_keep", strconv.Itoa(*in.BackupKeep)); err != nil {
			respond(w, nil, err)
			return
		}
	}
	writeJSON(w, a.currentSettings())
}

// ---- helpers ----

func actor(r *http.Request) string {
	name, err := url.PathUnescape(r.Header.Get("X-Device-Name"))
	if err != nil {
		return ""
	}
	return name
}

func isLocalRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request.")
		return false
	}
	return true
}

func respond(w http.ResponseWriter, v any, err error) {
	var inputErr InputError
	switch {
	case err == nil:
		writeJSON(w, v)
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "Not found.")
	case errors.Is(err, ErrExists):
		writeError(w, http.StatusConflict, "A bin with that code already exists.")
	case errors.As(err, &inputErr):
		writeError(w, http.StatusBadRequest, inputErr.msg)
	default:
		log.Printf("error: %v", err)
		writeError(w, http.StatusInternalServerError, "Something went wrong: "+err.Error())
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
