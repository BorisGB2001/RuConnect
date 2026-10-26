package api_test

import (
	"encoding/csv"
	"strings"
	"testing"
)

func TestAuditCSVExport(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 3; i++ {
		e.auditPost("conn", rawConn("555000111", 10+i, "77", nonce(), `"peer":["999","Ctrl, \"PC\""],"type":0`))
	}
	r := e.get("/api/admin/audit/conns?format=csv", e.admin)
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("content type %q", ct)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(r.Body), "\xEF\xBB\xBF"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[0][0] != "guid" || rows[1][3] != `Ctrl, "PC"` {
		t.Fatalf("csv rows: %q", rows)
	}
	if r := e.get("/api/admin/audit/files?format=csv", e.admin); r.Status != 200 {
		t.Fatalf("files csv: %s", r)
	}
}
