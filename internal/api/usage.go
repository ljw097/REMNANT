package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ljw097/remnant/internal/store"
)

// usage summarises token use over the last `days` days (0 or absent: all
// time), with days counted in the browser's IANA time zone `tz`.
func (s *Server) usage(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	var since *time.Time
	if d := v.Get("days"); d != "" && d != "0" {
		n, err := strconv.Atoi(d)
		if err != nil || n < 1 || n > 3650 {
			writeError(w, http.StatusBadRequest, "bad_request", "days must be 1–3650, or 0 for all time")
			return
		}
		tz := zone(v.Get("tz"))
		loc, _ := time.LoadLocation(tz)
		now := time.Now().In(loc)
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(n - 1))
		since = &start
	}
	u, err := s.store.Usage(r.Context(), since, zone(v.Get("tz")))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// zone returns tz when it is a known IANA zone name, else UTC.
func zone(tz string) string {
	if tz == "" || len(tz) > 64 {
		return "UTC"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return "UTC"
	}
	return tz
}

func (s *Server) getModelPrices(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.ModelPrices(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) setModelPrices(w http.ResponseWriter, r *http.Request) {
	var in map[string]store.ModelPrice
	if !decode(w, r, &in) {
		return
	}
	p, err := s.store.SetModelPrices(r.Context(), in, principal(r).UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
