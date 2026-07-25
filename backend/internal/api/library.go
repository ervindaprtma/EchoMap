package api

// Handlers for the "library" surfaces the device form needs: maps, icons, and
// per-device service launchers (Doc 5 §2–§4 subsets; the rest lands with their
// full pages).

import (
	"io"
	"net/http"
	"strings"

	"echomap/internal/store"
)

// ---- maps ----

func (s *server) listMaps(w http.ResponseWriter, r *http.Request) {
	maps, err := s.store.ListMaps(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": maps})
}

func (s *server) createMap(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		ParentMapID *int64 `json:"parent_map_id"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	m, err := s.store.CreateMap(r.Context(), strings.TrimSpace(body.Name), body.ParentMapID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

// ---- icons ----

const maxIconBytes = 512 << 10 // 512 KB is generous for an SVG/PNG glyph

func (s *server) listIcons(w http.ResponseWriter, r *http.Request) {
	icons, err := s.store.ListIcons(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": icons})
}

// createIcon accepts multipart/form-data: file + name (+ optional category).
func (s *server) createIcon(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxIconBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file field is required"})
		return
	}
	defer file.Close()

	var mime string
	switch name := strings.ToLower(hdr.Filename); {
	case strings.HasSuffix(name, ".svg"):
		mime = "image/svg+xml"
	case strings.HasSuffix(name, ".png"):
		mime = "image/png"
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "only .svg and .png are supported"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxIconBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxIconBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file empty or larger than 512KB"})
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = strings.TrimSuffix(hdr.Filename, ".svg")
		name = strings.TrimSuffix(name, ".png")
	}
	id, err := s.store.CreateIcon(r.Context(), name, strings.TrimSpace(r.FormValue("category")), mime, data)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": name, "mime": mime})
}

func (s *server) deleteIcon(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteIcon(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

// ---- device services ----

var validKinds = map[string]bool{"HTTP": true, "HTTPS": true, "SSH": true, "TELNET": true, "CUSTOM_URL": true}

func (s *server) listServices(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	items, err := s.store.ListServices(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) createService(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var body struct {
		Label string  `json:"label"`
		Kind  string  `json:"kind"`
		Port  *int    `json:"port"`
		Path  *string `json:"path"`
		URL   *string `json:"url"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Label) == "" || !validKinds[body.Kind] {
		writeJSON(w, http.StatusBadRequest,
			map[string]string{"error": "label and a valid kind (HTTP/HTTPS/SSH/TELNET/CUSTOM_URL) are required"})
		return
	}
	if body.Kind == "CUSTOM_URL" && (body.URL == nil || strings.TrimSpace(*body.URL) == "") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required for CUSTOM_URL services"})
		return
	}
	svc, err := s.store.CreateService(r.Context(), store.DeviceServiceRow{
		DeviceID: id, Label: strings.TrimSpace(body.Label), Kind: body.Kind,
		Port: body.Port, Path: body.Path, URL: body.URL,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, svc)
}

func (s *server) deleteService(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteService(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}
