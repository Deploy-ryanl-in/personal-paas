package auth

import (
	"testing"
	"time"
)

func TestClaimsBoundaries(t *testing.T) {
	now := time.Now()
	sha := "0123456789012345678901234567890123456789"
	v := Verifier{Policy: Policy{OwnerID: "337720882", Owner: "Deploy-ryanl-in", Audience: "https://deploy.ryanl.in", Workflows: map[string]string{"Deploy-ryanl-in/personal-paas/.github/workflows/release.yml@" + sha: sha}}}
	good := Claims{Iss: Issuer, Aud: v.Policy.Audience, Exp: now.Add(5 * time.Minute).Unix(), Iat: now.Unix(), Nbf: now.Unix(), JTI: "unique", RepositoryID: "42", Repository: "Deploy-ryanl-in/app", Owner: v.Policy.Owner, OwnerID: v.Policy.OwnerID, Ref: "refs/heads/main", SHA: sha, Event: "push", RunID: "22", WorkflowRef: "Deploy-ryanl-in/personal-paas/.github/workflows/release.yml@" + sha, WorkflowSHA: sha}
	if e := v.ValidateClaims(good, now); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*Claims){func(c *Claims) { c.OwnerID = "9" }, func(c *Claims) { c.Event = "pull_request" }, func(c *Claims) { c.WorkflowSHA = "other" }, func(c *Claims) { c.Ref = "refs/pull/1/merge" }, func(c *Claims) { c.Exp = now.Unix() - 1 }, func(c *Claims) { c.Aud = "other" }, func(c *Claims) { c.Repository = "attacker/app" }} {
		c := good
		mutate(&c)
		if e := v.ValidateClaims(c, now); e == nil {
			t.Fatal("accepted invalid claims", c)
		}
	}
}
