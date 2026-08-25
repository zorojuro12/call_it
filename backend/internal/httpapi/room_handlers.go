package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/zorojuro12/call_it/backend/internal/domain"
)

// registerRoomRoutes wires the room creation and joining routes onto mux.
func registerRoomRoutes(mux *http.ServeMux, d Deps) {
	mux.Handle("POST /api/v1/rooms", RequireAuth(d.Issuer)(http.HandlerFunc(handleCreateRoom(d))))
}

type createRoomRequest struct {
	BuyIn *int64 `json:"buy_in"`
}

type createRoomResponse struct {
	RoomID string `json:"room_id"`
	Code   string `json:"code"`
	BuyIn  int64  `json:"buy_in"`
	Token  string `json:"token"`
}

func handleCreateRoom(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := ClaimsFrom(r.Context())

		var req createRoomRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil || req.BuyIn == nil {
			WriteError(w, &APIError{Status: 400, Code: "validation_error", Message: "buy_in is required and must be an integer"})
			return
		}

		created, err := d.Rooms.Create(r.Context(), claims.UserID, claims.DisplayName, domain.Tokens(*req.BuyIn))
		if err != nil {
			WriteError(w, err)
			return
		}

		WriteData(w, 201, createRoomResponse{
			RoomID: created.RoomID,
			Code:   created.Code,
			BuyIn:  int64(created.BuyIn),
			Token:  created.Token,
		})
	}
}
