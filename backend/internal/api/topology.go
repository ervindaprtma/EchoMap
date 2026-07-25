package api

import (
	"net/http"
	"strconv"
	"strings"

	"echomap/internal/store"
)

// parseMapID reads ?map_id= (absent/empty = root map -> nil).
func parseMapID(w http.ResponseWriter, r *http.Request) (*int64, bool) {
	q := r.URL.Query().Get("map_id")
	if q == "" {
		return nil, true
	}
	id, err := strconv.ParseInt(q, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid map_id"})
		return nil, false
	}
	return &id, true
}

// GET /topology?map_id= — one canvas: device nodes, edges among them, and
// submap nodes with subtree-aggregated status counts (Doc 5 §3).
func (s *server) getTopology(w http.ResponseWriter, r *http.Request) {
	mapID, ok := parseMapID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	nodes, err := s.store.ListDevicesByMap(ctx, mapID)
	if err != nil {
		writeErr(w, err)
		return
	}
	edges, err := s.store.ListEdgesByMap(ctx, mapID)
	if err != nil {
		writeErr(w, err)
		return
	}
	submaps, err := s.store.SubmapSummaries(ctx, mapID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "edges": edges, "submaps": submaps})
}

// POST /topology/nodes/positions/bulk — "Save Layout".
func (s *server) savePositions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Nodes []struct {
			ID   int64   `json:"id"`
			PosX float64 `json:"pos_x"`
			PosY float64 `json:"pos_y"`
		} `json:"nodes"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if len(body.Nodes) == 0 {
		writeJSON(w, http.StatusOK, map[string]int{"updated": 0})
		return
	}
	positions := make([]store.NodePosition, 0, len(body.Nodes))
	for _, n := range body.Nodes {
		positions = append(positions, store.NodePosition{ID: n.ID, PosX: n.PosX, PosY: n.PosY})
	}
	updated, err := s.store.SavePositions(r.Context(), positions)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"updated": updated})
}

// POST /topology/edges — create a MANUAL link (link_type forced server-side).
func (s *server) createEdge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SourceDeviceID int64   `json:"source_device_id"`
		TargetDeviceID int64   `json:"target_device_id"`
		Label          *string `json:"label"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.SourceDeviceID == body.TargetDeviceID {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "source and target must differ"})
		return
	}
	if body.Label != nil && strings.TrimSpace(*body.Label) == "" {
		body.Label = nil
	}
	edge, err := s.store.CreateManualEdge(r.Context(), body.SourceDeviceID, body.TargetDeviceID, body.Label)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, edge)
}

// DELETE /topology/edges/{id} — MANUAL edges only; AUTO edges are derived.
func (s *server) deleteEdge(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteManualEdge(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

// PATCH /maps/{id} — move a submap node on its parent canvas.
// ponytail: position only; rename/re-parent land with the maps management UI.
func (s *server) patchMap(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var body struct {
		PosX *float64 `json:"pos_x"`
		PosY *float64 `json:"pos_y"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.PosX == nil || body.PosY == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pos_x and pos_y are required"})
		return
	}
	if err := s.store.UpdateMapPosition(r.Context(), id, *body.PosX, *body.PosY); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "pos_x": *body.PosX, "pos_y": *body.PosY})
}
