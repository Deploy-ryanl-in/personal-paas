package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCryptographicTokenValidation(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	sha := strings.Repeat("a", 40)
	ref := "Deploy-ryanl-in/personal-paas/.github/workflows/release.yml@" + sha
	v := Verifier{Policy: Policy{Audience: "https://deploy.ryanl.in", Owner: "Deploy-ryanl-in", OwnerID: "337720882", Workflows: map[string]string{ref: sha}}, keys: map[string]*rsa.PublicKey{"test": &key.PublicKey}, fetched: time.Now()}
	claims := Claims{Iss: Issuer, Aud: v.Policy.Audience, Iat: time.Now().Unix(), Nbf: time.Now().Unix(), Exp: time.Now().Add(5 * time.Minute).Unix(), JTI: "signed", Repository: "Deploy-ryanl-in/app", RepositoryID: "42", Owner: v.Policy.Owner, OwnerID: v.Policy.OwnerID, Ref: "refs/heads/main", SHA: sha, Event: "push", RunID: "123", WorkflowRef: ref, WorkflowSHA: sha}
	b, _ := json.Marshal(claims)
	h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test"}`))
	payload := h + "." + base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(payload))
	sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if e != nil {
		t.Fatal(e)
	}
	token := payload + "." + base64.RawURLEncoding.EncodeToString(sig)
	if _, e = v.Verify(context.Background(), token); e != nil {
		t.Fatal(e)
	}
	tampered := token[:len(token)-5] + "aaaaa"
	if _, e = v.Verify(context.Background(), tampered); e == nil {
		t.Fatal("accepted tampered signature")
	}
	none := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","kid":"test"}`)) + "." + strings.Split(token, ".")[1] + "."
	if _, e = v.Verify(context.Background(), none); e == nil {
		t.Fatal("accepted unsigned token")
	}
}
