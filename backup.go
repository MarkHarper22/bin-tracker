package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type BackupInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Created string `json:"created"`
	Kind    string `json:"kind"` // auto, manual, before-restore
}

const backupPrefix = "bintracker-"

// BackupNow writes a consistent copy of the database into the backups folder.
func (s *Store) BackupNow(kind string) (BackupInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.backupLocked(kind)
}

// backupLocked expects the caller to hold s.mu (read or write).
func (s *Store) backupLocked(kind string) (BackupInfo, error) {
	if err := os.MkdirAll(s.backupDir, 0o755); err != nil {
		return BackupInfo{}, err
	}
	stamp := time.Now().Format("20060102-150405")
	name := fmt.Sprintf("%s%s-%s.db", backupPrefix, stamp, kind)
	for i := 2; fileExists(filepath.Join(s.backupDir, name)); i++ {
		name = fmt.Sprintf("%s%s-%d-%s.db", backupPrefix, stamp, i, kind)
	}
	path := filepath.Join(s.backupDir, name)
	if _, err := s.db.Exec(`VACUUM INTO ?`, path); err != nil {
		return BackupInfo{}, err
	}
	_ = setSetting(s.db, "last_backup", time.Now().UTC().Format(time.RFC3339))
	_ = setSetting(s.db, "last_backup_marker", s.changeMarker())
	s.pruneBackups()
	return backupInfoFor(path)
}

// changeMarker identifies the database's state, so scheduled backups are
// skipped when nothing has changed since the last one.
func (s *Store) changeMarker() string {
	var id int64
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM history`).Scan(&id)
	return strconv.FormatInt(id, 10)
}

func (s *Store) ListBackups() ([]BackupInfo, error) {
	entries, err := os.ReadDir(s.backupDir)
	if os.IsNotExist(err) {
		return []BackupInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []BackupInfo{}
	for _, e := range entries {
		if e.IsDir() || !isBackupName(e.Name()) {
			continue
		}
		if info, err := backupInfoFor(filepath.Join(s.backupDir, e.Name())); err == nil {
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// pruneBackups keeps the newest N automatic backups (a setting) and the newest
// 10 of each other kind.
func (s *Store) pruneBackups() {
	all, err := s.ListBackups()
	if err != nil {
		return
	}
	keepAuto := 14
	if n, err := strconv.Atoi(getSetting(s.db, "backup_keep", "14")); err == nil && n > 0 {
		keepAuto = n
	}
	counts := map[string]int{}
	for _, b := range all { // newest first
		counts[b.Kind]++
		limit := 10
		if b.Kind == "auto" {
			limit = keepAuto
		}
		if counts[b.Kind] > limit {
			os.Remove(filepath.Join(s.backupDir, b.Name))
		}
	}
}

// RestoreBackup replaces the live database with a backup. A safety backup of
// the current data is made first so a restore can itself be undone.
func (s *Store) RestoreBackup(name, actor string) error {
	if !isBackupName(name) || filepath.Base(name) != name {
		return invalid("Unknown backup.")
	}
	src := filepath.Join(s.backupDir, name)
	if !fileExists(src) {
		return ErrNotFound
	}
	if err := checkBackupFile(src); err != nil {
		return invalid("That backup can't be read: %v", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	safety, err := s.backupLocked("before-restore")
	if err != nil {
		return fmt.Errorf("safety backup failed, nothing was changed: %w", err)
	}

	s.db.Close()
	if err := s.replaceDBFile(src); err != nil {
		// Put the pre-restore copy back so the app keeps working.
		log.Printf("restore failed (%v); reverting to %s", err, safety.Name)
		if rerr := s.replaceDBFile(filepath.Join(s.backupDir, safety.Name)); rerr != nil {
			log.Printf("revert also failed: %v", rerr)
		}
		return fmt.Errorf("restore failed: %w", err)
	}
	_ = addHistory(s.db, "", "Restored backup", name, actor)
	return nil
}

// replaceDBFile copies src over the database file and reopens it. The caller
// must hold the write lock and have closed s.db.
func (s *Store) replaceDBFile(src string) error {
	os.Remove(s.path + "-wal")
	os.Remove(s.path + "-shm")
	tmp := s.path + ".restoring"
	if err := copyFile(src, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return err
	}
	db, err := openDB(s.path)
	if err != nil {
		return err
	}
	s.db = db
	return nil
}

func checkBackupFile(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	var ok string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&ok); err != nil {
		return err
	}
	if ok != "ok" {
		return fmt.Errorf("database check failed: %s", ok)
	}
	var n int
	return db.QueryRow(`SELECT COUNT(*) FROM bins`).Scan(&n)
}

// RunBackupScheduler checks every 10 minutes whether an automatic backup is
// due (per the backup_hours setting; 0 disables them).
func (s *Store) RunBackupScheduler(ctx context.Context) {
	check := func() {
		hours := s.GetSettingInt("backup_hours", 24)
		if hours <= 0 {
			return
		}
		if last, err := time.Parse(time.RFC3339, s.GetSetting("last_backup", "")); err == nil && time.Since(last) < time.Duration(hours)*time.Hour {
			return
		}
		s.mu.RLock()
		marker := s.changeMarker()
		unchanged := marker == "0" || getSetting(s.db, "last_backup_marker", "") == marker
		s.mu.RUnlock()
		if unchanged {
			return
		}
		if info, err := s.BackupNow("auto"); err != nil {
			log.Printf("automatic backup failed: %v", err)
		} else {
			log.Printf("automatic backup saved: %s", info.Name)
		}
	}

	check()
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

func isBackupName(name string) bool {
	return strings.HasPrefix(name, backupPrefix) && strings.HasSuffix(name, ".db")
}

func backupInfoFor(path string) (BackupInfo, error) {
	st, err := os.Stat(path)
	if err != nil {
		return BackupInfo{}, err
	}
	name := filepath.Base(path)
	info := BackupInfo{Name: name, Size: st.Size(), Created: st.ModTime().UTC().Format(time.RFC3339), Kind: "manual"}
	base := strings.TrimSuffix(strings.TrimPrefix(name, backupPrefix), ".db")
	for _, kind := range []string{"before-restore", "auto", "manual"} {
		if strings.HasSuffix(base, "-"+kind) {
			info.Kind = kind
			break
		}
	}
	if len(base) >= 15 {
		if t, err := time.ParseInLocation("20060102-150405", base[:15], time.Local); err == nil {
			info.Created = t.UTC().Format(time.RFC3339)
		}
	}
	return info, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
