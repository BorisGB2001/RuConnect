package api

import (
	"net/http"

	"github.com/BorisGB2001/RuConnect/internal/store"
)

// The client's "Accessible devices" tab. Administrators see everything in
// their scope (an organization administrator: the organization); other
// users see the members of their user group, their own devices and the
// device groups granted to their user group.

func (s *Server) handleAccessibleGroups(w http.ResponseWriter, r *http.Request, u *store.User) {
	var uid int64
	if !u.IsAdmin {
		uid = u.ID
	}
	groups, total, err := s.store.ListDeviceGroups(r.Context(), u.OrgID, uid, pageOf(r))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	out := make([]map[string]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, map[string]string{"name": g.Name})
	}
	writeList(w, total, out)
}

func (s *Server) handleAccessibleUsers(w http.ResponseWriter, r *http.Request, u *store.User) {
	f := store.UserFilter{Org: u.OrgID}
	if r.URL.Query().Get("status") == "1" {
		st := store.UserNormal
		f.Status = &st
	}
	if !u.IsAdmin {
		f.OnlyID = u.ID
	}
	users, total, err := s.store.ListUsers(r.Context(), f, pageOf(r))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	out := make([]userPayload, 0, len(users))
	for _, x := range users {
		out = append(out, toPayload(x))
	}
	writeList(w, total, out)
}

type peerPayload struct {
	ID              string            `json:"id"`
	Status          int               `json:"status"`
	User            string            `json:"user"`
	UserName        string            `json:"user_name"`
	DeviceGroupName string            `json:"device_group_name"`
	Note            string            `json:"note"`
	Info            map[string]string `json:"info"`
}

func (s *Server) handleAccessiblePeers(w http.ResponseWriter, r *http.Request, u *store.User) {
	f := store.DeviceFilter{Org: u.OrgID}
	if !u.IsAdmin {
		f.AccessibleTo = u.ID
	}
	devices, total, err := s.store.ListDevices(r.Context(), f, pageOf(r))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	ids := make([]string, 0, len(devices))
	for _, d := range devices {
		ids = append(ids, d.ID)
	}
	pr, err := s.newPeerResolver(r, ids)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	out := make([]peerPayload, 0, len(devices))
	for _, d := range devices {
		out = append(out, peerPayload{
			ID:              pr.address(d.ID),
			Status:          1,
			User:            d.UserName,
			UserName:        d.UserName,
			DeviceGroupName: d.GroupName,
			Note:            d.Note,
			Info: map[string]string{
				"username":    d.Username,
				"os":          d.OS,
				"device_name": d.Hostname,
			},
		})
	}
	writeList(w, total, out)
}
