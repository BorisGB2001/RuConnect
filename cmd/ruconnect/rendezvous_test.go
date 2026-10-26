package main

import (
	"context"
	"testing"
	"time"

	"github.com/BorisGB2001/RuConnect/internal/rendezvous"
	"github.com/BorisGB2001/RuConnect/internal/store"
)

func TestAuthorizeConnection(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, _ := st.SaveOrganization(ctx, "", store.OrganizationInput{Name: "A", Domains: []string{"a.example"}})
	b, _ := st.SaveOrganization(ctx, "", store.OrganizationInput{Name: "B"})
	token := func(name string, org int64) string {
		u, err := st.CreateUser(ctx, store.UserInput{Name: name, Password: "password1", Status: store.UserNormal, OrgID: org})
		if err != nil {
			t.Fatal(err)
		}
		tok, _ := st.CreateToken(ctx, store.Token{UserID: u.ID}, time.Hour)
		return tok
	}
	alice, bob, staff := token("alice", a.ID), token("bob", b.ID), token("staff", 0)
	for id, org := range map[string]int64{"100000001": a.ID, "100000002": b.ID, "100000003": 0} {
		st.UpsertSysInfo(ctx, store.SysInfo{ID: id, UUID: "u"})
		st.ClaimDeviceOrganization(ctx, id, org)
	}
	cases := []struct {
		token, peer string
		ok          bool
	}{
		{alice, "100000001", true},
		{alice, "100000002", false},
		{alice, "100000003", false},
		{alice, "999999999", false}, // unknown to the panel
		{bob, "100000002", true},
		{staff, "100000002", true},
		{"", "100000002", true},      // not logged in
		{"bogus", "100000002", true}, // invalid token counts as not logged in
	}
	for _, c := range cases {
		reason := authorizeConnection(ctx, st, rendezvous.Request{Token: c.token, PeerID: c.peer})
		if (reason == "") != c.ok {
			t.Errorf("token %.8s -> %s: %q", c.token, c.peer, reason)
		}
	}
}
