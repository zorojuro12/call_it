package httpapi

import (
	"encoding/json"
	"net/http"
)

// registerAuthRoutes wires POST /api/v1/auth/register and
// POST /api/v1/auth/login onto mux.
func registerAuthRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("POST /api/v1/auth/register", handleRegister(d))
}

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type accountResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Balance     int64  `json:"balance"`
}

func handleRegister(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registerRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			WriteError(w, &APIError{Status: 400, Code: "validation_error", Message: "malformed request body"})
			return
		}

		acct, token, err := d.Accounts.Register(r.Context(), req.Email, req.Password, req.DisplayName)
		if err != nil {
			WriteError(w, err)
			return
		}

		WriteData(w, 201, struct {
			Account accountResponse `json:"account"`
			Token   string          `json:"token"`
		}{
			Account: accountResponse{
				ID:          acct.ID,
				Email:       acct.Email,
				DisplayName: acct.DisplayName,
				Balance:     int64(acct.Balance),
			},
			Token: token,
		})
	}
}
