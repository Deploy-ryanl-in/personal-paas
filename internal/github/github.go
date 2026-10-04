package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type Client struct {
	HTTP   *http.Client
	AppID  string
	Key    *rsa.PrivateKey
	mu     sync.Mutex
	tokens map[int64]token
}
type token struct {
	Value   string
	Expires time.Time
}

func LoadKey(b []byte) (*rsa.PrivateKey, error) {
	p, _ := pem.Decode(b)
	if p == nil {
		return nil, errors.New("invalid App key")
	}
	if k, e := x509.ParsePKCS1PrivateKey(p.Bytes); e == nil {
		return k, nil
	}
	k, e := x509.ParsePKCS8PrivateKey(p.Bytes)
	if e != nil {
		return nil, e
	}
	r, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("App key must be RSA")
	}
	return r, nil
}
func (c *Client) jwt() (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	b, _ := json.Marshal(map[string]any{"iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(8 * time.Minute).Unix(), "iss": c.AppID})
	s := header + "." + base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(s))
	sig, e := rsa.SignPKCS1v15(rand.Reader, c.Key, crypto.SHA256, h[:])
	return s + "." + base64.RawURLEncoding.EncodeToString(sig), e
}
func (c *Client) request(ctx context.Context, method, path, tok string, body any, out any) error {
	var b io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		b = bytes.NewReader(data)
	}
	req, _ := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, b)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	r, e := c.HTTP.Do(req)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return fmt.Errorf("GitHub HTTP %d", r.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(out)
	}
	return nil
}
func (c *Client) Token(ctx context.Context, repo string, id int64) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t := c.tokens[id]; time.Until(t.Expires) > 5*time.Minute {
		return t.Value, nil
	}
	jwt, e := c.jwt()
	if e != nil {
		return "", e
	}
	var install struct {
		ID int64 `json:"id"`
	}
	if e = c.request(ctx, "GET", "/repos/"+repo+"/installation", jwt, nil, &install); e != nil {
		return "", e
	}
	var t struct {
		Token   string    `json:"token"`
		Expires time.Time `json:"expires_at"`
	}
	e = c.request(ctx, "POST", fmt.Sprintf("/app/installations/%d/access_tokens", install.ID), jwt, map[string]any{"repository_ids": []int64{id}, "permissions": map[string]string{"contents": "read", "actions": "read"}}, &t)
	if e != nil {
		return "", e
	}
	if c.tokens == nil {
		c.tokens = map[int64]token{}
	}
	c.tokens[id] = token{t.Token, t.Expires}
	return t.Token, nil
}
func (c *Client) Identity(ctx context.Context, repo string, id int64) (manifest.Identity, error) {
	var m manifest.Identity
	t, e := c.Token(ctx, repo, id)
	if e != nil {
		return m, e
	}
	var r struct {
		ID            int64
		Name          string
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
		Owner         struct {
			ID    int64
			Login string
		}
	}
	e = c.request(ctx, "GET", "/repos/"+repo, t, nil, &r)
	m = manifest.Identity{ID: r.ID, OwnerID: r.Owner.ID, Owner: r.Owner.Login, Name: r.Name, FullName: r.FullName, DefaultBranch: r.DefaultBranch}
	if e == nil && r.ID != id {
		e = errors.New("immutable repository ID mismatch")
	}
	return m, e
}
func (c *Client) Manifest(ctx context.Context, id manifest.Identity, ref string) (manifest.Manifest, error) {
	var m manifest.Manifest
	t, e := c.Token(ctx, id.FullName, id.ID)
	if e != nil {
		return m, e
	}
	var f struct {
		Encoding string
		Content  string
		Size     int
	}
	e = c.request(ctx, "GET", "/repos/"+id.FullName+"/contents/paas.json?ref="+url.QueryEscape(ref), t, nil, &f)
	if e != nil {
		return m, e
	}
	if f.Encoding != "base64" || f.Size > 65536 {
		return m, errors.New("invalid manifest file")
	}
	b, e := base64.StdEncoding.DecodeString(f.Content)
	if e != nil {
		return m, e
	}
	m, e = manifest.Decode(b)
	if e == nil {
		e = m.Validate()
	}
	return m, e
}
func (c *Client) CheckRun(ctx context.Context, id manifest.Identity, runID, sha, ref, event string) error {
	t, e := c.Token(ctx, id.FullName, id.ID)
	if e != nil {
		return e
	}
	var r struct {
		HeadSHA        string `json:"head_sha"`
		HeadBranch     string `json:"head_branch"`
		Event          string
		Status         string
		Repository     struct{ ID int64 }
		HeadRepository struct{ ID int64 } `json:"head_repository"`
	}
	if _, e = strconv.ParseUint(runID, 10, 64); e != nil {
		return e
	}
	e = c.request(ctx, "GET", "/repos/"+id.FullName+"/actions/runs/"+runID, t, nil, &r)
	if e != nil {
		return e
	}
	if r.HeadSHA != sha || "refs/heads/"+r.HeadBranch != ref || r.Event != event || r.Repository.ID != id.ID || r.HeadRepository.ID != id.ID {
		return errors.New("workflow run source mismatch")
	}
	return nil
}
