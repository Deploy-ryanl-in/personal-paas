package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const Issuer = "https://token.actions.githubusercontent.com"

type Policy struct {
	Audience            string            `json:"audience"`
	OwnerID             string            `json:"ownerId"`
	Owner               string            `json:"owner"`
	RepositoryAllowlist []string          `json:"repositoryAllowlist"`
	Workflows           map[string]string `json:"workflows"`
	DatabaseImages      map[string]string `json:"databaseImages"`
	HelperImage         string            `json:"helperImage"`
}
type Claims struct {
	Iss          string `json:"iss"`
	Aud          string `json:"aud"`
	Sub          string `json:"sub"`
	Exp          int64  `json:"exp"`
	Iat          int64  `json:"iat"`
	Nbf          int64  `json:"nbf"`
	JTI          string `json:"jti"`
	Repository   string `json:"repository"`
	RepositoryID string `json:"repository_id"`
	Owner        string `json:"repository_owner"`
	OwnerID      string `json:"repository_owner_id"`
	Ref          string `json:"ref"`
	SHA          string `json:"sha"`
	Event        string `json:"event_name"`
	WorkflowRef  string `json:"job_workflow_ref"`
	WorkflowSHA  string `json:"job_workflow_sha"`
	RunID        string `json:"run_id"`
	RunAttempt   string `json:"run_attempt"`
}
type Verifier struct {
	Policy  Policy
	Client  *http.Client
	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

func (v *Verifier) Verify(ctx context.Context, token string) (Claims, error) {
	var c Claims
	if len(token) > 16384 {
		return c, errors.New("token too large")
	}
	p := strings.Split(token, ".")
	if len(p) != 3 {
		return c, errors.New("malformed token")
	}
	var h struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	b, e := base64.RawURLEncoding.DecodeString(p[0])
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &h); e != nil || h.Alg != "RS256" || h.Kid == "" {
		return c, errors.New("invalid token algorithm")
	}
	key, e := v.key(ctx, h.Kid)
	if e != nil {
		return c, e
	}
	sig, e := base64.RawURLEncoding.DecodeString(p[2])
	if e != nil {
		return c, e
	}
	sum := sha256.Sum256([]byte(p[0] + "." + p[1]))
	if e = rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); e != nil {
		return c, errors.New("invalid token signature")
	}
	b, e = base64.RawURLEncoding.DecodeString(p[1])
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, e
	}
	if e = v.ValidateClaims(c, time.Now()); e != nil {
		return c, e
	}
	return c, nil
}
func (v *Verifier) ValidateClaims(c Claims, now time.Time) error {
	if c.Iss != Issuer || c.Aud != v.Policy.Audience || c.Exp <= now.Unix() || c.Nbf > now.Add(30*time.Second).Unix() || c.Iat > now.Add(30*time.Second).Unix() || c.Iat < now.Add(-10*time.Minute).Unix() || c.Exp-c.Iat > 10*60 || c.JTI == "" {
		return errors.New("invalid token issuer/audience/lifetime")
	}
	id, e := strconv.ParseInt(c.RepositoryID, 10, 64)
	if e != nil || id <= 0 {
		return errors.New("invalid repository ID")
	}
	parts := strings.Split(c.Repository, "/")
	if len(parts) != 2 || parts[0] != c.Owner {
		return errors.New("inconsistent repository identity")
	}
	trusted := c.OwnerID == v.Policy.OwnerID && strings.EqualFold(c.Owner, v.Policy.Owner)
	for _, s := range v.Policy.RepositoryAllowlist {
		if s == c.RepositoryID {
			trusted = true
		}
	}
	if !trusted {
		return errors.New("untrusted repository")
	}
	if c.Event != "push" && c.Event != "workflow_dispatch" {
		return errors.New("event not allowed")
	}
	if !strings.HasPrefix(c.Ref, "refs/heads/") || len(c.SHA) != 40 {
		return errors.New("branch/commit claim missing")
	}
	if _, e := strconv.ParseUint(c.RunID, 10, 64); e != nil {
		return errors.New("invalid run ID")
	}
	expected, ok := v.Policy.Workflows[c.WorkflowRef]
	if !ok || len(expected) != 40 || c.WorkflowSHA != expected {
		return errors.New("unapproved reusable workflow revision")
	}
	return nil
}
func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if time.Since(v.fetched) < time.Hour {
		if k := v.keys[kid]; k != nil {
			return k, nil
		}
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", Issuer+"/.well-known/jwks", nil)
	r, e := v.Client.Do(req)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, errors.New("JWKS unavailable")
	}
	var set struct {
		Keys []struct{ Kid, Kty, N, E string }
	}
	if e = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&set); e != nil {
		return nil, e
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		n, e1 := base64.RawURLEncoding.DecodeString(k.N)
		exp, e2 := base64.RawURLEncoding.DecodeString(k.E)
		if e1 != nil || e2 != nil {
			continue
		}
		ei := 0
		for _, b := range exp {
			ei = ei<<8 + int(b)
		}
		if len(n) >= 256 && ei >= 3 {
			keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: ei}
		}
	}
	v.keys = keys
	v.fetched = time.Now()
	if k := keys[kid]; k != nil {
		return k, nil
	}
	return nil, fmt.Errorf("unknown signing key")
}
