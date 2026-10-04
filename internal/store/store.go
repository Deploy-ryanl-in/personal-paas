package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"io"
	_ "modernc.org/sqlite"
	"time"
)

type Store struct {
	DB     *sql.DB
	Cipher cipher.AEAD
}
type Release struct {
	ID      string            `json:"id"`
	Repo    manifest.Identity `json:"repository"`
	Commit  string            `json:"commit"`
	Run     int64             `json:"runId"`
	Config  manifest.Manifest `json:"config"`
	Images  map[string]string `json:"images"`
	Secrets map[string]string `json:"-"`
	Created time.Time         `json:"created"`
	Volumes map[string]string `json:"volumes,omitempty"`
}
type Job struct {
	ID        string    `json:"id"`
	RepoID    int64     `json:"repositoryId"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Created   string    `json:"created"`
	Release   Release   `json:"release"`
	Operation Operation `json:"operation,omitempty"`
}
type Operation struct {
	Action    string `json:"action"`
	ReleaseID string `json:"releaseId,omitempty"`
	Service   string `json:"service,omitempty"`
	Image     string `json:"image,omitempty"`
	BackupID  string `json:"backupId,omitempty"`
	Confirm   string `json:"confirm,omitempty"`
}

func Open(file string, key []byte) (*Store, error) {
	if len(key) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", file)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db, Cipher: a}
	_, e = db.Exec(`PRAGMA journal_mode=WAL;PRAGMA synchronous=FULL;PRAGMA foreign_keys=ON;PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS jobs(id TEXT PRIMARY KEY,repo INTEGER NOT NULL,kind TEXT NOT NULL,status TEXT NOT NULL,payload BLOB NOT NULL,error TEXT NOT NULL DEFAULT '',created TEXT NOT NULL,updated TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS releases(id TEXT PRIMARY KEY,repo INTEGER NOT NULL,payload BLOB NOT NULL,created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS apps(repo INTEGER PRIMARY KEY,active TEXT NOT NULL DEFAULT '',latest_run INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS domains(domain TEXT PRIMARY KEY,repo INTEGER NOT NULL,service TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS nonces(jti TEXT PRIMARY KEY,expires INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS backups(id TEXT PRIMARY KEY,repo INTEGER NOT NULL,release_id TEXT NOT NULL,created TEXT NOT NULL,path TEXT NOT NULL);
UPDATE jobs SET status='queued' WHERE status='running';`)
	return s, e
}
func (s *Store) Seal(b []byte) []byte {
	n := make([]byte, s.Cipher.NonceSize())
	if _, e := io.ReadFull(rand.Reader, n); e != nil {
		panic(e)
	}
	return s.Cipher.Seal(n, n, b, nil)
}
func (s *Store) Unseal(b []byte) ([]byte, error) {
	n := s.Cipher.NonceSize()
	if len(b) < n {
		return nil, errors.New("invalid encrypted record")
	}
	return s.Cipher.Open(nil, b[:n], b[n:], nil)
}
func (s *Store) encode(r Release) ([]byte, error) {
	b, e := json.Marshal(struct {
		Release Release
		Secrets map[string]string
	}{r, r.Secrets})
	if e != nil {
		return nil, e
	}
	return s.Seal(b), nil
}
func (s *Store) decode(b []byte) (Release, error) {
	var v struct {
		Release Release
		Secrets map[string]string
	}
	d, e := s.Unseal(b)
	if e != nil {
		return v.Release, e
	}
	e = json.Unmarshal(d, &v)
	v.Release.Secrets = v.Secrets
	return v.Release, e
}
func (s *Store) Nonce(jti string, expires int64) error {
	_, e := s.DB.Exec("INSERT INTO nonces(jti,expires) VALUES(?,?)", jti, expires)
	return e
}
func (s *Store) Domains(id int64) (map[string]string, error) {
	rows, e := s.DB.Query("SELECT service,domain FROM domains WHERE repo=?", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var n, d string
		if e = rows.Scan(&n, &d); e != nil {
			return nil, e
		}
		m[n] = d
	}
	return m, rows.Err()
}
func (s *Store) CheckDomains(id int64, m manifest.Manifest) error {
	for _, v := range m.Services {
		if v.Type != "web" {
			continue
		}
		var owner int64
		e := s.DB.QueryRow("SELECT repo FROM domains WHERE domain=?", v.Domain).Scan(&owner)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		if e == nil && owner != id {
			return errors.New("domain belongs to another repository")
		}
	}
	return nil
}
func (s *Store) Enqueue(r Release, kind string, op Operation) (Job, error) {
	operationRun := int64(0)
	if kind == "operation" {
		operationRun = r.Run
	}
	b, _ := json.Marshal(struct {
		Repo         int64
		Commit       string
		Config       manifest.Manifest
		Images       map[string]string
		Secrets      map[string]string
		Op           Operation
		Kind         string
		OperationRun int64 `json:",omitempty"`
	}{r.Repo.ID, r.Commit, r.Config, r.Images, r.Secrets, op, kind, operationRun})
	sum := sha256.Sum256(b)
	id := hex.EncodeToString(sum[:])
	r.ID = id
	r.Created = time.Now().UTC()
	data, e := s.encode(r)
	if e != nil {
		return Job{}, e
	}
	ob, _ := json.Marshal(op)
	payload, _ := json.Marshal(struct {
		Data []byte
		Op   json.RawMessage
	}{data, ob})
	tx, e := s.DB.Begin()
	if e != nil {
		return Job{}, e
	}
	defer tx.Rollback()
	_, e = tx.Exec("INSERT OR IGNORE INTO apps(repo) VALUES(?)", r.Repo.ID)
	if e != nil {
		return Job{}, e
	}
	var latest int64
	e = tx.QueryRow("SELECT latest_run FROM apps WHERE repo=?", r.Repo.ID).Scan(&latest)
	if e != nil {
		return Job{}, e
	}
	if kind == "deploy" && r.Run < latest {
		return Job{}, errors.New("stale workflow run")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, e = tx.Exec("INSERT OR IGNORE INTO jobs(id,repo,kind,status,payload,created,updated) VALUES(?,?,?,'queued',?,?,?)", id, r.Repo.ID, kind, payload, now, now)
	if e != nil {
		return Job{}, e
	}
	if kind == "deploy" {
		_, e = tx.Exec("UPDATE jobs SET payload=?,updated=? WHERE id=? AND status IN ('queued','running')", payload, now, id)
		if e != nil {
			return Job{}, e
		}
		_, e = tx.Exec("UPDATE apps SET latest_run=MAX(latest_run,?) WHERE repo=?", r.Run, r.Repo.ID)
		if e != nil {
			return Job{}, e
		}
	}
	if e = tx.Commit(); e != nil {
		return Job{}, e
	}
	return s.Job(id, r.Repo.ID)
}
func (s *Store) Job(id string, repo int64) (Job, error) {
	var j Job
	var payload []byte
	e := s.DB.QueryRow("SELECT id,repo,kind,status,payload,error,created FROM jobs WHERE id=? AND repo=?", id, repo).Scan(&j.ID, &j.RepoID, &j.Kind, &j.Status, &payload, &j.Error, &j.Created)
	if e != nil {
		return j, e
	}
	var p struct {
		Data []byte
		Op   Operation
	}
	if e = json.Unmarshal(payload, &p); e != nil {
		return j, e
	}
	j.Release, e = s.decode(p.Data)
	j.Operation = p.Op
	if e == nil && j.Status == "succeeded" {
		if actual, err := s.Release(j.ID, repo); err == nil {
			j.Release = actual
		}
	}
	return j, e
}
func (s *Store) Next() (Job, error) {
	var id string
	var repo int64
	e := s.DB.QueryRow("SELECT id,repo FROM jobs WHERE status='queued' ORDER BY created LIMIT 1").Scan(&id, &repo)
	if e != nil {
		return Job{}, e
	}
	j, e := s.Job(id, repo)
	if e != nil {
		return j, e
	}
	_, e = s.DB.Exec("UPDATE jobs SET status='running',updated=? WHERE id=?", time.Now().UTC().Format(time.RFC3339Nano), id)
	return j, e
}
func (s *Store) Finish(id, status string, err error) error {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	_, e := s.DB.Exec("UPDATE jobs SET status=?,error=?,updated=? WHERE id=?", status, msg, time.Now().UTC().Format(time.RFC3339Nano), id)
	return e
}
func (s *Store) IsStale(r Release) bool {
	var latest int64
	if err := s.DB.QueryRow("SELECT latest_run FROM apps WHERE repo=?", r.Repo.ID).Scan(&latest); err != nil {
		return true
	}
	if r.Run >= latest {
		return false
	}
	// A newer workflow requesting the identical running deployment reuses it.
	// It must not supersede its own candidate; a different deployment still does.
	j, err := s.Job(r.ID, r.Repo.ID)
	return err != nil || j.Release.Run != latest
}
func (s *Store) Active(repo int64) (Release, error) {
	var id string
	e := s.DB.QueryRow("SELECT active FROM apps WHERE repo=?", repo).Scan(&id)
	if e != nil || id == "" {
		return Release{}, sql.ErrNoRows
	}
	return s.Release(id, repo)
}
func (s *Store) LatestRelease(repo int64) (Release, error) {
	var id string
	if err := s.DB.QueryRow("SELECT id FROM releases WHERE repo=? ORDER BY created DESC LIMIT 1", repo).Scan(&id); err != nil {
		return Release{}, err
	}
	return s.Release(id, repo)
}
func (s *Store) Release(id string, repo int64) (Release, error) {
	var b []byte
	e := s.DB.QueryRow("SELECT payload FROM releases WHERE id=? AND repo=?", id, repo).Scan(&b)
	if e != nil {
		return Release{}, e
	}
	return s.decode(b)
}
func (s *Store) Activate(r Release) error {
	b, e := s.encode(r)
	if e != nil {
		return e
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec("INSERT OR REPLACE INTO releases(id,repo,payload,created) VALUES(?,?,?,?)", r.ID, r.Repo.ID, b, r.Created.Format(time.RFC3339Nano))
	if e != nil {
		return e
	}
	_, e = tx.Exec("DELETE FROM domains WHERE repo=?", r.Repo.ID)
	if e != nil {
		return e
	}
	for name, v := range r.Config.Services {
		if v.Type == "web" {
			_, e = tx.Exec("INSERT INTO domains(domain,repo,service) VALUES(?,?,?)", v.Domain, r.Repo.ID, name)
			if e != nil {
				return e
			}
		}
	}
	_, e = tx.Exec("UPDATE apps SET active=? WHERE repo=?", r.ID, r.Repo.ID)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Stop(repo int64) error {
	_, e := s.DB.Exec("UPDATE apps SET active='' WHERE repo=?", repo)
	return e
}
func (s *Store) ActiveAll() ([]Release, error) {
	rows, e := s.DB.Query("SELECT releases.payload FROM apps JOIN releases ON apps.active=releases.id")
	if e != nil {
		return nil, e
	}
	var data [][]byte
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			rows.Close()
			return nil, e
		}
		data = append(data, b)
	}
	e = rows.Err()
	rows.Close()
	var out []Release
	for _, b := range data {
		r, err := s.decode(b)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, e
}
func (s *Store) History(repo int64) ([]map[string]any, error) {
	rows, e := s.DB.Query("SELECT id,kind,status,error,created FROM jobs WHERE repo=? ORDER BY created DESC LIMIT 100", repo)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, k, st, err, c string
		if e = rows.Scan(&id, &k, &st, &err, &c); e != nil {
			return nil, e
		}
		out = append(out, map[string]any{"id": id, "kind": k, "status": st, "error": err, "created": c})
	}
	return out, rows.Err()
}
func (s *Store) Prune() error {
	_, e := s.DB.Exec("DELETE FROM nonces WHERE expires<?; DELETE FROM jobs WHERE status IN ('failed','succeeded','superseded') AND created<?", time.Now().Unix(), time.Now().Add(-90*24*time.Hour).UTC().Format(time.RFC3339Nano))
	return e
}
func (s *Store) String() string { return fmt.Sprint("encrypted SQLite state") }
