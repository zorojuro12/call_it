// This file is package ws_test, not ws: it needs internal/httpapi for
// the REST half of the flow, and httpapi imports internal/ws — an
// internal test file (package ws) pulling in httpapi would be a build
// cycle. An external test package avoids it, at the cost of using only
// ws's exported surface and defining its own small Redis test-store
// helper rather than reusing testmain_test.go's unexported one.
package ws_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/zorojuro12/call_it/backend/internal/account"
	"github.com/zorojuro12/call_it/backend/internal/auth"
	"github.com/zorojuro12/call_it/backend/internal/httpapi"
	"github.com/zorojuro12/call_it/backend/internal/redisstore"
	"github.com/zorojuro12/call_it/backend/internal/room"
	"github.com/zorojuro12/call_it/backend/internal/round"
	"github.com/zorojuro12/call_it/backend/internal/wager"
	"github.com/zorojuro12/call_it/backend/internal/ws"
)

// e2eTestDB mirrors every package's own testmain_test.go: DB 15, never
// 0. This file doesn't flush it — package ws's own TestMain (internal
// test file, same test binary) already does that before any test in
// this directory runs.
const e2eTestDB = 15

func e2eTestStore(t *testing.T) *redisstore.Store {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	store, err := redisstore.New(addr, e2eTestDB)
	if err != nil {
		t.Fatalf("redisstore.New: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("closing store: %v", err)
		}
	})
	return store
}

var e2eIDCounter atomic.Uint64

func e2eTestID(t *testing.T, kind string) string {
	t.Helper()
	n := e2eIDCounter.Add(1)
	return fmt.Sprintf("%s-%s-%d", kind, t.Name(), n)
}

func e2eReadUntil(t *testing.T, conn *websocket.Conn, wantType string, timeout time.Duration) json.RawMessage {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage() waiting for %q: %v", wantType, err)
		}
		env, err := ws.Decode(raw)
		if err != nil {
			t.Fatalf("Decode(): %v", err)
		}
		if env.Type == wantType {
			return env.Data
		}
	}
}

// e2eDataEnvelope unwraps httpapi's {"data": ...} success envelope.
func e2eDataEnvelope(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response envelope: %v", err)
	}
	if err := json.Unmarshal(env.Data, v); err != nil {
		t.Fatalf("decode response data: %v", err)
	}
}

func e2ePostJSON(t *testing.T, url, token string, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return resp
}

// e2eSendEnvelope marshals data, wraps it in an Envelope of msgType,
// and writes it to conn as a text message.
func e2eSendEnvelope(t *testing.T, conn *websocket.Conn, msgType string, data any) {
	t.Helper()
	payload, err := ws.Encode(msgType, data)
	if err != nil {
		t.Fatalf("Encode(%s): %v", msgType, err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("WriteMessage(%s): %v", msgType, err)
	}
}

// e2eTokenUserID recovers the user ID a room-scoped token carries, so
// the test can match round_resolved rows without threading the guest
// ID separately through every REST response.
func e2eTokenUserID(t *testing.T, token string, issuer *auth.Issuer) string {
	t.Helper()
	claims, err := issuer.Verify(token)
	if err != nil {
		t.Fatalf("issuer.Verify: %v", err)
	}
	return claims.UserID
}

// TestEndToEndRound plays a full round over the real socket transport
// against a real hub, real round/wager services, and real Redis — the
// phase's real acceptance test (Task 10 CP1). Everything before the
// final token-conservation check is setup.
func TestEndToEndRound(t *testing.T) {
	store := e2eTestStore(t)
	issuer, err := auth.NewIssuer([]byte("01234567890123456789012345678901"), time.Hour)
	if err != nil {
		t.Fatalf("auth.NewIssuer() = %v, want nil", err)
	}
	hub := ws.NewHub()

	accounts := account.NewService(store, issuer)
	rooms := room.NewService(store, issuer)
	restMux := httpapi.NewMux(httpapi.Deps{Accounts: accounts, Rooms: rooms, Store: store, Issuer: issuer, Hub: hub})
	restServer := httptest.NewServer(restMux)
	defer restServer.Close()

	roundSvc := round.NewService(context.Background(), store, hub)
	wagerSvc := wager.NewService(store, hub)
	router := ws.NewRouter(roundSvc, wagerSvc)
	wsMux := http.NewServeMux()
	wsMux.Handle("GET /api/v1/socket", ws.Handler(hub, issuer, ws.DefaultClientConfig(), router.Handle))
	wsServer := httptest.NewServer(wsMux)
	defer wsServer.Close()
	wsBase := strings.Replace(wsServer.URL, "http://", "ws://", 1) + "/api/v1/socket?token="

	// 1. Host registers and creates a room over REST.
	hostEmail := e2eTestID(t, "host") + "@example.com"
	regResp := e2ePostJSON(t, restServer.URL+"/api/v1/auth/register", "", map[string]string{
		"email": hostEmail, "password": "correct horse battery", "display_name": "Host",
	})
	if regResp.StatusCode != 201 {
		t.Fatalf("register status = %d, want 201", regResp.StatusCode)
	}
	var regData struct {
		Token string `json:"token"`
	}
	e2eDataEnvelope(t, regResp, &regData)

	createResp := e2ePostJSON(t, restServer.URL+"/api/v1/rooms", regData.Token, map[string]any{"buy_in": 1000})
	if createResp.StatusCode != 201 {
		t.Fatalf("create room status = %d, want 201", createResp.StatusCode)
	}
	var created struct {
		RoomID string `json:"room_id"`
		Code   string `json:"code"`
		Token  string `json:"token"`
	}
	e2eDataEnvelope(t, createResp, &created)

	// 2. Two players join by code over REST, as guests.
	joinA := e2ePostJSON(t, restServer.URL+"/api/v1/rooms/"+created.Code+"/participants", "", map[string]string{"display_name": "Alice"})
	if joinA.StatusCode != 201 {
		t.Fatalf("join A status = %d, want 201", joinA.StatusCode)
	}
	var joinedA struct {
		Token          string `json:"token"`
		SessionBalance int64  `json:"session_balance"`
	}
	e2eDataEnvelope(t, joinA, &joinedA)

	joinB := e2ePostJSON(t, restServer.URL+"/api/v1/rooms/"+created.Code+"/participants", "", map[string]string{"display_name": "Bob"})
	if joinB.StatusCode != 201 {
		t.Fatalf("join B status = %d, want 201", joinB.StatusCode)
	}
	var joinedB struct {
		Token          string `json:"token"`
		SessionBalance int64  `json:"session_balance"`
	}
	e2eDataEnvelope(t, joinB, &joinedB)

	// 3. All three open sockets with their room-scoped tokens.
	hostConn, _, err := websocket.DefaultDialer.Dial(wsBase+created.Token, nil)
	if err != nil {
		t.Fatalf("host Dial: %v", err)
	}
	defer hostConn.Close()
	aConn, _, err := websocket.DefaultDialer.Dial(wsBase+joinedA.Token, nil)
	if err != nil {
		t.Fatalf("A Dial: %v", err)
	}
	defer aConn.Close()
	bConn, _, err := websocket.DefaultDialer.Dial(wsBase+joinedB.Token, nil)
	if err != nil {
		t.Fatalf("B Dial: %v", err)
	}
	defer bConn.Close()

	// 4. Host sends create_round; all three receive round_opened. The
	// lock window must clear round.MinLockIn (3s) — Open validates it
	// for real here, unlike the timer package's own tests, which
	// bypass Open for a faster sub-3s window.
	e2eSendEnvelope(t, hostConn, "create_round", map[string]any{
		"question": "Clutch?", "outcomes": []string{"Yes", "No"}, "lock_in_ms": 3000,
	})
	for _, conn := range []*websocket.Conn{hostConn, aConn, bConn} {
		e2eReadUntil(t, conn, "round_opened", 2*time.Second)
	}

	// 5. A wagers 100 on outcome 0; B wagers 200 on outcome 1. Each
	// produces an odds_updated all three receive.
	e2eSendEnvelope(t, aConn, "place_wager", map[string]any{
		"outcome": 0, "amount": 100, "idempotency_key": uuid.NewString(),
	})
	for _, conn := range []*websocket.Conn{hostConn, aConn, bConn} {
		e2eReadUntil(t, conn, "odds_updated", 2*time.Second)
	}

	e2eSendEnvelope(t, bConn, "place_wager", map[string]any{
		"outcome": 1, "amount": 200, "idempotency_key": uuid.NewString(),
	})
	var lastOdds json.RawMessage
	for _, conn := range []*websocket.Conn{hostConn, aConn, bConn} {
		lastOdds = e2eReadUntil(t, conn, "odds_updated", 2*time.Second)
	}
	var oddsData struct {
		Bettors int `json:"bettors"`
		Players int `json:"players"`
	}
	if err := json.Unmarshal(lastOdds, &oddsData); err != nil {
		t.Fatalf("decode odds_updated: %v", err)
	}
	if oddsData.Bettors != 2 || oddsData.Players != 2 {
		t.Errorf("final odds_updated Bettors/Players = %d/%d, want 2/2", oddsData.Bettors, oddsData.Players)
	}

	// 6. All three receive round_locked (lock window is 3s).
	for _, conn := range []*websocket.Conn{hostConn, aConn, bConn} {
		e2eReadUntil(t, conn, "round_locked", 5*time.Second)
	}

	// 7. Host resolves; all three receive round_resolved.
	e2eSendEnvelope(t, hostConn, "resolve_round", map[string]any{"winning_outcome": 0})
	var lastResolved json.RawMessage
	for _, conn := range []*websocket.Conn{hostConn, aConn, bConn} {
		lastResolved = e2eReadUntil(t, conn, "round_resolved", 2*time.Second)
	}
	var resolvedData struct {
		Results []struct {
			UserID string `json:"user_id"`
			Net    int64  `json:"net"`
		} `json:"results"`
		Dust int64 `json:"dust"`
	}
	if err := json.Unmarshal(lastResolved, &resolvedData); err != nil {
		t.Fatalf("decode round_resolved: %v", err)
	}
	userA := e2eTokenUserID(t, joinedA.Token, issuer)
	userB := e2eTokenUserID(t, joinedB.Token, issuer)
	var netA, netB int64
	for _, row := range resolvedData.Results {
		switch row.UserID {
		case userA:
			netA = row.Net
		case userB:
			netB = row.Net
		}
	}
	if netA != 200 {
		t.Errorf("A's Net = %d, want 200", netA)
	}
	if netB != -200 {
		t.Errorf("B's Net = %d, want -200", netB)
	}

	// 8. Token conservation: A's wallet + B's wallet + Dust equals
	// their combined opening stakes. This is the phase's real
	// acceptance test — everything before it is setup.
	balanceA, err := store.Balance(context.Background(), created.RoomID, userA)
	if err != nil {
		t.Fatalf("Balance(A) = %v, want nil", err)
	}
	balanceB, err := store.Balance(context.Background(), created.RoomID, userB)
	if err != nil {
		t.Fatalf("Balance(B) = %v, want nil", err)
	}
	gotSum := int64(balanceA) + int64(balanceB) + resolvedData.Dust
	wantSum := joinedA.SessionBalance + joinedB.SessionBalance
	if gotSum != wantSum {
		t.Errorf("A+B wallets + dust = %d, want %d (combined opening stakes)", gotSum, wantSum)
	}
}
