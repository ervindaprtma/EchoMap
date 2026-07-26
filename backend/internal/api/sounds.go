package api

import (
	"io"
	"net/http"
	"strings"

	"echomap/internal/store"
)

const maxSoundBytes = 1 << 20 // 1 MB (Pillar 14)

func (s *server) listSounds(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListSounds(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// getSoundAudio streams the raw .wav bytes. Fetched by the browser with an
// authenticated fetch → blob → Audio (an <audio src> can't send the cookie).
func (s *server) getSoundAudio(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	data, err := s.store.GetSoundAudio(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(data)
}

// createSound accepts multipart/form-data: file (.wav ≤ 1 MB) + optional name.
func (s *server) createSound(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxSoundBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file field is required"})
		return
	}
	defer file.Close()
	if !strings.HasSuffix(strings.ToLower(hdr.Filename), ".wav") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "only .wav files are supported"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSoundBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxSoundBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file empty or larger than 1MB"})
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = hdr.Filename[:len(hdr.Filename)-len(".wav")]
	}
	id, err := s.store.CreateSound(r.Context(), name, data)
	if err != nil {
		writeAuthErr(w, err) // maps ErrConflict → 409 (duplicate name)
		return
	}
	s.logEvent(r, "NOTICE", "config", "uploaded alert sound "+name, nil, nil)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": name})
}

func (s *server) deleteSound(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteSound(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

func (s *server) getSoundAssignments(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.GetSoundAssignments(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// putSoundAssignments assigns a sound id (or null) to each event class.
func (s *server) putSoundAssignments(w http.ResponseWriter, r *http.Request) {
	var a store.SoundAssignments
	if !readJSON(w, r, &a) {
		return
	}
	if err := s.store.UpdateSoundAssignments(r.Context(), a); err != nil {
		if strings.Contains(err.Error(), "foreign key") {
			unprocessable(w, "assigned a sound id that does not exist")
			return
		}
		writeErr(w, err)
		return
	}
	s.logEvent(r, "NOTICE", "config", "updated alert sound assignments", nil, nil)
	writeJSON(w, http.StatusOK, a)
}
