package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func openMem(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mkUser(t *testing.T, s *Store, name string, admin bool) *User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), UserInput{Name: name, Password: "pw-" + name, Status: UserNormal, IsAdmin: admin})
	if err != nil {
		t.Fatalf("CreateUser %s: %v", name, err)
	}
	return u
}

func TestMigrationsIdempotentOnReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "dir", "ruconnect.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	u := mkUser(t, s, "alice", true)
	if err := s.SetSetting(ctx, "k", "v"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	for i := 0; i < 2; i++ {
		s, err = Open(path)
		if err != nil {
			t.Fatalf("reopen %d: %v", i, err)
		}
		var version, rows int
		if err := s.db.QueryRow(`SELECT version, (SELECT COUNT(*) FROM schema_version) FROM schema_version`).Scan(&version, &rows); err != nil {
			t.Fatal(err)
		}
		if version != len(migrations) || rows != 1 {
			t.Errorf("schema_version = %d (%d rows), want %d (1 row)", version, rows, len(migrations))
		}
		got, err := s.UserByName(ctx, "ALICE")
		if err != nil || got.GUID != u.GUID || !got.CheckPassword("pw-alice") {
			t.Errorf("user lost on reopen: %v %v", got, err)
		}
		if v, _ := s.GetSetting(ctx, "k", ""); v != "v" {
			t.Errorf("setting lost: %q", v)
		}
		s.Close()
	}
}

func TestUserBasics(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	mkUser(t, s, "Bob", false)
	if _, err := s.CreateUser(ctx, UserInput{Name: "bob", Status: UserNormal}); !errors.Is(err, ErrConflict) {
		t.Errorf("case-insensitive duplicate: err = %v, want ErrConflict", err)
	}
	if _, err := s.CreateUser(ctx, UserInput{Name: "  "}); err == nil {
		t.Error("blank name accepted")
	}
	u, err := s.CreateUser(ctx, UserInput{Name: "nopw", Status: UserNormal})
	if err != nil {
		t.Fatal(err)
	}
	if u.CheckPassword("") {
		t.Error("user without password accepts the empty password")
	}
	if _, err := s.UserByGUID(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByGUID missing: %v", err)
	}
}

func TestTokens(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	u := mkUser(t, s, "tok", false)

	raw, err := s.CreateToken(ctx, Token{UserID: u.ID}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := s.UserByToken(ctx, raw); err != nil || got.ID != u.ID {
		t.Fatalf("UserByToken: %v %v", got, err)
	}
	if _, _, err := s.UserByToken(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty token: %v", err)
	}

	expired, _ := s.CreateToken(ctx, Token{UserID: u.ID}, -time.Hour)
	if _, _, err := s.UserByToken(ctx, expired); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired token: %v", err)
	}

	// Changing the password signs the user out everywhere.
	if _, err := s.UpdateUser(ctx, u.GUID, UserInput{Name: u.Name, Password: "new-password", Status: UserNormal}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.UserByToken(ctx, raw); !errors.Is(err, ErrNotFound) {
		t.Errorf("token survived password change: %v", err)
	}

	// A disabled user's token is rejected even if it still exists.
	raw2, _ := s.CreateToken(ctx, Token{UserID: u.ID}, time.Hour)
	if _, err := s.db.Exec(`UPDATE users SET status = 0 WHERE id = ?`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.UserByToken(ctx, raw2); !errors.Is(err, ErrNotFound) {
		t.Errorf("disabled user's token accepted: %v", err)
	}

	if err := s.DeleteExpiredTokens(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCountActiveAdmins(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	a := mkUser(t, s, "a", true)
	b := mkUser(t, s, "b", true)
	mkUser(t, s, "c", false)
	if n, _ := s.CountActiveAdmins(ctx, a.ID); n != 1 {
		t.Errorf("other admins = %d, want 1", n)
	}
	s.db.Exec(`UPDATE users SET status = 0 WHERE id = ?`, b.ID)
	if n, _ := s.CountActiveAdmins(ctx, a.ID); n != 0 {
		t.Errorf("disabled admin counted: %d", n)
	}
}

func connRows(t *testing.T, s *Store) []*ConnAudit {
	t.Helper()
	rows, _, err := s.ListConnAudits(context.Background(), AuditFilter{}, Page{})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestRecordConnEventFolding(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	ev := func(action, session string) ConnEvent {
		e := ConnEvent{DeviceID: "dev", DeviceUUID: "u", ConnID: 7, SessionID: session, Action: action, ConnType: -1}
		if action == "" {
			e.PeerID, e.PeerName, e.ConnType, e.PrimaryAuth = "ctrl", "Ctrl", 1, 3
		}
		if action == "new" {
			e.IP = "10.0.0.1"
		}
		return e
	}
	must := func(e ConnEvent) {
		t.Helper()
		if err := s.RecordConnEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	// new -> authorized -> close folds into one row.
	must(ev("new", "42"))
	must(ev("", "42"))
	must(ev("close", "42"))
	rows := connRows(t, s)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.IP != "10.0.0.1" || r.PeerID != "ctrl" || r.ConnType != 1 || r.PrimaryAuth != 3 || r.AuthorizedAt == 0 || r.ClosedAt == 0 {
		t.Errorf("folded row = %+v", r)
	}

	// close with no open row is a no-op.
	must(ev("close", "42"))
	if n := len(connRows(t, s)); n != 1 {
		t.Errorf("stray close created rows: %d", n)
	}

	// A "new" without session id followed by an authorized record fills the session id in.
	must(ev("new", ""))
	must(ev("", "43"))
	rows = connRows(t, s)
	if len(rows) != 2 || rows[0].SessionID != "43" || rows[0].IP != "10.0.0.1" {
		t.Errorf("session id not adopted: %+v", rows[0])
	}

	// A second "new" for the same conn id and session while one is open closes the stale row.
	must(ev("new", "43"))
	rows = connRows(t, s)
	if len(rows) != 3 || rows[1].ClosedAt == 0 || rows[0].ClosedAt != 0 {
		t.Errorf("stale row not closed: %+v / %+v", rows[1], rows[0])
	}

	// An authorized record with no prior "new" creates its own row.
	e := ev("", "50")
	e.ConnID = 99
	must(e)
	rows = connRows(t, s)
	if len(rows) != 4 || rows[0].ConnID != 99 || rows[0].AuthorizedAt == 0 {
		t.Errorf("authorized-only row = %+v", rows[0])
	}

	// Session lookup and notes.
	g, err := s.ConnGUIDBySession(ctx, "dev", "50", "")
	if err != nil || g != rows[0].GUID {
		t.Errorf("ConnGUIDBySession = %q %v", g, err)
	}
	if g, _ := s.ConnGUIDBySession(ctx, "dev", "nope", ""); g != "" {
		t.Errorf("unknown session guid = %q", g)
	}
	if err := s.SetConnNoteBySession(ctx, "dev", "50", "hi"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetConnNote(ctx, "missing", "x", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetConnNote missing: %v", err)
	}
	if connRows(t, s)[0].Note != "hi" {
		t.Error("note not set")
	}
}

func TestPruneAudit(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	s.RecordConnEvent(ctx, ConnEvent{DeviceID: "d", ConnID: 1, Action: "new"})
	s.AddFileAudit(ctx, FileAudit{DeviceID: "d"})
	s.AddAlarmAudit(ctx, AlarmAudit{DeviceID: "d", Typ: 1})
	if err := s.PruneAudit(ctx, now()+10); err != nil {
		t.Fatal(err)
	}
	if n := len(connRows(t, s)); n != 0 {
		t.Errorf("conn rows after prune = %d", n)
	}
	f, _, _ := s.ListFileAudits(ctx, AuditFilter{}, Page{})
	a, _, _ := s.ListAlarmAudits(ctx, AuditFilter{}, Page{})
	if len(f) != 0 || len(a) != 0 {
		t.Errorf("file/alarm rows after prune = %d/%d", len(f), len(a))
	}
}

func peerTags(t *testing.T, s *Store, abID int64) map[string][]string {
	t.Helper()
	peers, _, err := s.ListABPeers(context.Background(), abID, Page{})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, p := range peers {
		out[p.ID] = p.Tags
	}
	return out
}

func TestRenameAndDeleteABTags(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	u := mkUser(t, s, "owner", false)
	ab, err := s.PersonalAB(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := s.CreateSharedAB(ctx, "Other", u.ID, "", "")

	for _, n := range []string{"a", "c"} {
		if err := s.AddABTag(ctx, ab.ID, ABTag{Name: n, Color: 7}); err != nil {
			t.Fatal(err)
		}
	}
	s.AddABTag(ctx, other.ID, ABTag{Name: "a"})
	s.AddABPeer(ctx, ab.ID, &ABPeer{ID: "1", Tags: []string{"a", "b"}})
	s.AddABPeer(ctx, ab.ID, &ABPeer{ID: "2", Tags: []string{"b", "a", "c"}})
	s.AddABPeer(ctx, ab.ID, &ABPeer{ID: "3"})
	s.AddABPeer(ctx, other.ID, &ABPeer{ID: "1", Tags: []string{"a"}})

	// Renaming "a" to "b" (a name some peers already carry) must not duplicate it.
	if err := s.RenameABTag(ctx, ab.ID, "a", "b"); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"1": {"b"}, "2": {"b", "c"}, "3": {}}
	if got := peerTags(t, s, ab.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("after rename: %v, want %v", got, want)
	}
	if got := peerTags(t, s, other.ID); !reflect.DeepEqual(got["1"], []string{"a"}) {
		t.Errorf("rename leaked into another book: %v", got)
	}
	tags, _ := s.ListABTags(ctx, ab.ID)
	if !reflect.DeepEqual(tags, []ABTag{{Name: "b", Color: 7}, {Name: "c", Color: 7}}) {
		t.Errorf("tags = %v", tags)
	}

	if err := s.RenameABTag(ctx, ab.ID, "b", "c"); !errors.Is(err, ErrConflict) {
		t.Errorf("rename onto existing tag: %v, want ErrConflict", err)
	}
	if err := s.RenameABTag(ctx, ab.ID, "zzz", "y"); !errors.Is(err, ErrNotFound) {
		t.Errorf("rename missing tag: %v, want ErrNotFound", err)
	}
	if err := s.RenameABTag(ctx, ab.ID, "b", " "); err == nil {
		t.Error("rename to blank accepted")
	}
	// A failed rename leaves peers untouched.
	if got := peerTags(t, s, ab.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("failed rename changed peers: %v", got)
	}

	if err := s.DeleteABTags(ctx, ab.ID, []string{"b"}); err != nil {
		t.Fatal(err)
	}
	want = map[string][]string{"1": {}, "2": {"c"}, "3": {}}
	if got := peerTags(t, s, ab.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("after delete: %v, want %v", got, want)
	}
	if err := s.UpdateABTagColor(ctx, ab.ID, ABTag{Name: "b", Color: 1}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update deleted tag: %v", err)
	}
}

func TestABPeerUpsertAndPartialUpdate(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	u := mkUser(t, s, "o", false)
	ab, _ := s.PersonalAB(ctx, u.ID)
	if again, _ := s.PersonalAB(ctx, u.ID); again.GUID != ab.GUID || again.Rule != RuleFull {
		t.Errorf("PersonalAB not stable: %v vs %v", again, ab)
	}

	if err := s.AddABPeer(ctx, ab.ID, &ABPeer{ID: " "}); err == nil {
		t.Error("blank peer id accepted")
	}
	s.AddABPeer(ctx, ab.ID, &ABPeer{ID: "1", Alias: "x", Hostname: "h"})
	if err := s.UpdateABPeer(ctx, ab.ID, "1", map[string]string{"alias": "y"}); err != nil {
		t.Fatal(err)
	}
	peers, _, _ := s.ListABPeers(ctx, ab.ID, Page{})
	if peers[0].Alias != "y" || peers[0].Hostname != "h" || peers[0].ForceAlwaysRelay != "false" {
		t.Errorf("partial update = %+v", peers[0])
	}
	if err := s.UpdateABPeer(ctx, ab.ID, "nope", map[string]string{"alias": "y"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update missing peer: %v", err)
	}
	if err := s.UpdateABPeer(ctx, ab.ID, "1", nil); err != nil {
		t.Errorf("empty update: %v", err)
	}
	// Re-adding replaces the fields.
	s.AddABPeer(ctx, ab.ID, &ABPeer{ID: "1", Alias: "z"})
	peers, total, _ := s.ListABPeers(ctx, ab.ID, Page{})
	if total != 1 || peers[0].Alias != "z" || peers[0].Hostname != "" {
		t.Errorf("re-add = %d %+v", total, peers[0])
	}
}

func TestABForUserRules(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	owner := mkUser(t, s, "owner", false)
	admin := mkUser(t, s, "admin", true)
	reader := mkUser(t, s, "reader", false)
	stranger := mkUser(t, s, "stranger", false)
	ab, _ := s.CreateSharedAB(ctx, "Team", owner.ID, "", "")
	personal, _ := s.PersonalAB(ctx, owner.ID)
	if err := s.SetABRule(ctx, ab.ID, reader.ID, RuleRead); err != nil {
		t.Fatal(err)
	}
	if err := s.SetABRule(ctx, ab.ID, reader.ID, 9); err == nil {
		t.Error("invalid rule accepted")
	}

	cases := []struct {
		u    *User
		guid string
		rule int // -1: not found
	}{
		{owner, ab.GUID, RuleFull}, {admin, ab.GUID, RuleFull}, {reader, ab.GUID, RuleRead}, {stranger, ab.GUID, -1},
		{owner, personal.GUID, RuleFull}, {admin, personal.GUID, -1}, {reader, personal.GUID, -1},
	}
	for _, c := range cases {
		got, err := s.ABForUser(ctx, c.guid, c.u)
		if c.rule == -1 {
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("%s on %s: err = %v, want ErrNotFound", c.u.Name, c.guid, err)
			}
			continue
		}
		if err != nil || got.Rule != c.rule {
			t.Errorf("%s on %s: rule = %v err = %v, want %d", c.u.Name, c.guid, got, err, c.rule)
		}
	}

	list, total, _ := s.SharedABsForUser(ctx, reader, Page{})
	if total != 1 || list[0].Rule != RuleRead {
		t.Errorf("reader list = %d %+v", total, list)
	}
	if _, total, _ := s.SharedABsForUser(ctx, stranger, Page{}); total != 0 {
		t.Errorf("stranger sees %d books", total)
	}
	if _, total, _ := s.SharedABsForUser(ctx, nil, Page{}); total != 1 {
		t.Errorf("panel list must hold only the shared book, got %d", total)
	}

	// Deleting the owner deletes their books (cascade).
	if err := s.DeleteUser(ctx, owner.GUID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ABByGUID(ctx, ab.GUID); !errors.Is(err, ErrNotFound) {
		t.Errorf("book survived owner deletion: %v", err)
	}
}

func TestDevicesHeartbeatAndAssign(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	if known, err := s.Heartbeat(ctx, "d1", "u1", "1.1.1.1", 0); err != nil || known {
		t.Errorf("unknown device heartbeat: %v %v", known, err)
	}
	if created, err := s.UpsertSysInfo(ctx, SysInfo{ID: "d1", UUID: "u1", Hostname: "h"}); err != nil || !created {
		t.Fatal(created, err)
	}
	// Another machine cannot take over the id (uuid is bound on first use).
	if _, err := s.UpsertSysInfo(ctx, SysInfo{ID: "d1", UUID: "evil", Hostname: "x"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("takeover: %v", err)
	}
	if known, _ := s.Heartbeat(ctx, "d1", "u1", "1.1.1.1", 2); !known {
		t.Error("known device not recognised")
	}
	if known, _ := s.Heartbeat(ctx, "d1", "other", "1.1.1.1", 0); known {
		t.Error("uuid mismatch treated as known")
	}
	d, _ := s.GetDevice(ctx, "d1")
	guid := d.GUID
	// Re-upload keeps the guid and assignments.
	u := mkUser(t, s, "x", false)
	// A wrong uuid cannot claim the device.
	s.AssignDeviceUserIfUnset(ctx, "d1", "evil", u.ID)
	if d, _ := s.GetDevice(ctx, "d1"); d.UserID != nil {
		t.Fatal("device claimed with a wrong uuid")
	}
	if err := s.AssignDeviceUserIfUnset(ctx, "d1", "u1", u.ID); err != nil {
		t.Fatal(err)
	}
	v := mkUser(t, s, "y", false)
	s.AssignDeviceUserIfUnset(ctx, "d1", "u1", v.ID)
	s.UpsertSysInfo(ctx, SysInfo{ID: "d1", UUID: "u1", Hostname: "h2"})
	d, _ = s.GetDevice(ctx, "d1")
	if d.GUID != guid || d.Hostname != "h2" || d.UserName != "x" || d.Conns != 2 {
		t.Errorf("device after re-upload = %+v", d)
	}
	zero := int64(0)
	if err := s.UpdateDevice(ctx, "d1", DeviceUpdate{UserID: &zero}); err != nil {
		t.Fatal(err)
	}
	if d, _ = s.GetDevice(ctx, "d1"); d.UserID != nil {
		t.Errorf("user not cleared: %v", *d.UserID)
	}
	if err := s.UpdateDevice(ctx, "missing", DeviceUpdate{UserID: &zero}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update missing: %v", err)
	}
	if err := s.UpdateDevice(ctx, "missing", DeviceUpdate{}); err != nil {
		t.Errorf("empty update: %v", err)
	}
}

func TestPageLimitOffset(t *testing.T) {
	cases := []struct {
		p             Page
		limit, offset int
	}{
		{Page{}, 100, 0},
		{Page{Current: 1, PageSize: 100}, 100, 0},
		{Page{Current: 3, PageSize: 10}, 10, 20},
		{Page{Current: -1, PageSize: 5000}, 100, 0},
	}
	for _, c := range cases {
		if l, o := c.p.limitOffset(); l != c.limit || o != c.offset {
			t.Errorf("%+v -> %d,%d want %d,%d", c.p, l, o, c.limit, c.offset)
		}
	}
}

func TestLegacyAB(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	u := mkUser(t, s, "l", false)
	if d, _, err := s.LegacyAB(ctx, u.ID); err != nil || d != "" {
		t.Errorf("empty legacy = %q %v", d, err)
	}
	s.SetLegacyAB(ctx, u.ID, `{"a":1}`)
	s.SetLegacyAB(ctx, u.ID, `{"a":2}`)
	if d, ts, _ := s.LegacyAB(ctx, u.ID); d != `{"a":2}` || ts == 0 {
		t.Errorf("legacy = %q %d", d, ts)
	}
}

// After a client restart conn ids start again, and the new process's "new"
// record (sent before login) carries session_id 0. It must still close a stale
// open row of the same device and conn id, and the following authorized record
// must fold into the new row.
func TestRecordConnEventSessionZeroNew(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	s.RecordConnEvent(ctx, ConnEvent{DeviceID: "dev", ConnID: 1, SessionID: "0", Action: "new", IP: "10.0.0.1"})
	s.RecordConnEvent(ctx, ConnEvent{DeviceID: "dev", ConnID: 1, SessionID: "77", PeerID: "p", ConnType: 0})
	rows := connRows(t, s)
	if len(rows) != 1 {
		t.Fatalf("regression: 'new' with session_id \"0\" and authorized with the real session id produce %d rows "+
			"(internal/store/audit.go openConn); want 1", len(rows))
	}
	if rows[0].IP != "10.0.0.1" || rows[0].SessionID != "77" {
		t.Errorf("folded row = %+v", rows[0])
	}
	s.RecordConnEvent(ctx, ConnEvent{DeviceID: "dev", ConnID: 1, SessionID: "0", Action: "new"})
	if rows := connRows(t, s); len(rows) != 2 || rows[1].ClosedAt == 0 {
		t.Errorf("stale row not closed by a restarted client's new record: %+v", rows)
	}
}
