package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/BorisGB2001/RuConnect/internal/store"
)

// CSV export of audit logs: GET /api/admin/audit/{conns,files,alarms}?format=csv
// honours the same filters as the JSON listing and returns every match.

const exportPage = 1000

func wantCSV(r *http.Request) bool { return r.URL.Query().Get("format") == "csv" }

func csvTime(ts int64) string {
	if ts == 0 {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format(time.RFC3339)
}

// csvSafe neutralizes cells that spreadsheet apps would run as formulas;
// device-reported fields (peer names, paths) are attacker-controlled.
func csvSafe(row []string) []string {
	for i, c := range row {
		if c != "" && strings.ContainsRune("=+-@\t\r", rune(c[0])) {
			row[i] = "'" + c
		}
	}
	return row
}

func startCSV(w http.ResponseWriter, name string, header []string) *csv.Writer {
	// Large exports may take longer than the server's write timeout.
	http.NewResponseController(w).SetWriteDeadline(time.Now().Add(15 * time.Minute))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ruconnect-%s-%s.csv"`, name, time.Now().UTC().Format("20060102-150405")))
	w.Write([]byte("\xEF\xBB\xBF")) // BOM, so spreadsheet apps detect UTF-8
	cw := csv.NewWriter(w)
	cw.Write(header)
	return cw
}

// exportAll pages through a listing function until every row is written.
func exportAll[T any](r *http.Request, list func(store.Page) ([]T, int, error), row func(T) []string, cw *csv.Writer) error {
	for page := 1; ; page++ {
		items, _, err := list(store.Page{Current: page, PageSize: exportPage})
		if err != nil {
			return err
		}
		for _, it := range items {
			if err := cw.Write(csvSafe(row(it))); err != nil {
				return err
			}
		}
		cw.Flush()
		if len(items) < exportPage || r.Context().Err() != nil {
			return cw.Error()
		}
	}
}

func (s *Server) exportConns(w http.ResponseWriter, r *http.Request) {
	f := auditFilter(r)
	cw := startCSV(w, "connections", []string{"guid", "device_id", "peer_id", "peer_name", "ip", "conn_type", "primary_auth",
		"two_factor", "session_id", "started", "authorized", "closed", "duration_seconds", "note"})
	err := exportAll(r, func(p store.Page) ([]*store.ConnAudit, int, error) { return s.store.ListConnAudits(r.Context(), f, p) },
		func(a *store.ConnAudit) []string {
			dur := ""
			if a.ClosedAt != 0 {
				dur = strconv.FormatInt(a.ClosedAt-a.CreatedAt, 10)
			}
			return []string{a.GUID, a.DeviceID, a.PeerID, a.PeerName, a.IP, strconv.Itoa(a.ConnType), strconv.Itoa(a.PrimaryAuth),
				strconv.Itoa(a.TwoFactor), a.SessionID, csvTime(a.CreatedAt), csvTime(a.AuthorizedAt), csvTime(a.ClosedAt), dur, a.Note}
		}, cw)
	if err != nil {
		s.log.Error("csv export", "err", err)
	}
}

func (s *Server) exportFiles(w http.ResponseWriter, r *http.Request) {
	f := auditFilter(r)
	cw := startCSV(w, "file-transfers", []string{"time", "device_id", "peer_id", "peer_name", "peer_ip", "direction", "path", "is_file", "files", "largest_files"})
	err := exportAll(r, func(p store.Page) ([]*store.FileAudit, int, error) { return s.store.ListFileAudits(r.Context(), f, p) },
		func(a *store.FileAudit) []string {
			dir := "device_to_controller"
			if a.Type == 1 {
				dir = "controller_to_device"
			}
			return []string{csvTime(a.CreatedAt), a.DeviceID, a.PeerID, a.PeerName, a.PeerIP, dir, a.Path,
				strconv.FormatBool(a.IsFile), strconv.FormatInt(a.Num, 10), a.Files}
		}, cw)
	if err != nil {
		s.log.Error("csv export", "err", err)
	}
}

func (s *Server) exportAlarms(w http.ResponseWriter, r *http.Request) {
	f := auditFilter(r)
	cw := startCSV(w, "alarms", []string{"time", "device_id", "conn_id", "type", "ip", "info"})
	err := exportAll(r, func(p store.Page) ([]*store.AlarmAudit, int, error) {
		return s.store.ListAlarmAudits(r.Context(), f, p)
	},
		func(a *store.AlarmAudit) []string {
			return []string{csvTime(a.CreatedAt), a.DeviceID, strconv.FormatInt(a.ConnID, 10), strconv.Itoa(a.Typ), a.IP, a.Info}
		}, cw)
	if err != nil {
		s.log.Error("csv export", "err", err)
	}
}
