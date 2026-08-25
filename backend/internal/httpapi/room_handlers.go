package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/domain"
	"github.com/zorojuro12/call_it/backend/internal/room"
)

// registerRoomRoutes wires the room creation and joining routes onto mux.
func registerRoomRoutes(mux *http.ServeMux, d Deps) {
	mux.Handle("POST /api/v1/rooms", RequireAuth(d.Issuer)(http.HandlerFunc(handleCreateRoom(d))))
	mux.Handle("POST /api/v1/rooms/{code}/participants", OptionalAuth(d.Issuer)(http.HandlerFunc(handleJoinRoom(d))))
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

type joinRoomRequest struct {
	DisplayName string `json:"display_name"`
}

type joinRoomResponse struct {
	RoomID         string `json:"room_id"`
	Guest          bool   `json:"guest"`
	SessionBalance int64  `json:"session_balance"`
	PartialBuyIn   bool   `json:"partial_buy_in"`
	Token          string `json:"token"`
}

func handleJoinRoom(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.PathValue("code")
		claims, hasAccount := ClaimsFrom(r.Context())

		var req joinRoomRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			WriteError(w, &APIError{Status: 400, Code: "validation_error", Message: "malformed request body"})
			return
		}

		joinReq := room.JoinRequest{Guest: !hasAccount}

		if hasAccount {
			acct, err := d.Store.User(r.Context(), claims.UserID)
			if err != nil {
				WriteError(w, err)
				return
			}
			joinReq.UserID = acct.ID
			joinReq.DisplayName = acct.DisplayName
			joinReq.AccountBalance = acct.Balance
		} else {
			normalized := auth.NormalizeDisplayName(req.DisplayName)
			if err := auth.ValidateDisplayName(normalized); err != nil {
				WriteError(w, err)
				return
			}
			joinReq.DisplayName = normalized
		}

		joined, err := d.Rooms.Join(r.Context(), code, joinReq)
		if err != nil {
			WriteError(w, err)
			return
		}

		WriteData(w, 201, joinRoomResponse{
			RoomID:         joined.RoomID,
			Guest:          joined.Guest,
			SessionBalance: int64(joined.SessionBalance),
			PartialBuyIn:   joined.PartialBuyIn,
			Token:          joined.Token,
		})
	}
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
