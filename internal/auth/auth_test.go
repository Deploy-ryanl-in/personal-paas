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

func TestPersonalAndOrganizationBindings(t *testing.T) {
	now := time.Now()
	sha := "0123456789012345678901234567890123456789"
	ref := "NewOwner/platform/.github/workflows/release.yml@" + sha
	v := Verifier{Policy: Policy{Audience: "https://ship.example.net", Domain: "example.net", Owners: []Owner{{ID: "93820487", Login: "RyanStanLin"}, {ID: "337720882", Login: "Deploy-ryanl-in"}}, Workflows: map[string]string{ref: sha}}}
	if err := v.Policy.Validate(); err != nil {
		t.Fatal(err)
	}
	c := Claims{Iss: Issuer, Aud: v.Policy.Audience, Exp: now.Add(5 * time.Minute).Unix(), Iat: now.Unix(), Nbf: now.Unix(), JTI: "personal", RepositoryID: "42", Repository: "RyanStanLin/private-demo", Owner: "RyanStanLin", OwnerID: "93820487", Ref: "refs/heads/main", SHA: sha, Event: "push", RunID: "22", WorkflowRef: ref, WorkflowSHA: sha}
	if err := v.ValidateClaims(c, now); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Claims){func(c *Claims) { c.OwnerID = "337720882" }, func(c *Claims) { c.Owner = "Other"; c.Repository = "Other/private-demo" }, func(c *Claims) { c.OwnerID = "999" }, func(c *Claims) { c.Aud = "https://deploy.ryanl.in" }, func(c *Claims) { c.Event = "pull_request" }} {
		bad := c
		change(&bad)
		if v.ValidateClaims(bad, now) == nil {
			t.Fatal("accepted mismatched personal identity")
		}
	}
	c.Owner = "Deploy-ryanl-in"
	c.OwnerID = "337720882"
	c.Repository = "Deploy-ryanl-in/other"
	if err := v.ValidateClaims(c, now); err != nil {
		t.Fatal(err)
	}
}
