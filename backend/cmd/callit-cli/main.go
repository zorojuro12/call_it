// Command callit-cli is a demo client that plays a full round against a
// live CallIt server — not a product surface, a Phase 4b acceptance
// vehicle (parent plan's "playable end to end from a CLI client").
// Carries no unit tests, consistent with cmd/api's documented 0%
// coverage: thin wiring, no branching logic worth testing in isolation.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "server address (host:port)")
	host := flag.Bool("host", false, "register, create a room, and host it")
	join := flag.String("join", "", "join an existing room by its code, as a guest")
	flag.Parse()

	if *host == (*join != "") {
		fmt.Fprintln(os.Stderr, "specify exactly one of --host or --join <code>")
		os.Exit(1)
	}

	stdin := bufio.NewReader(os.Stdin)
	base := "http://" + *addr

	var token, roomID string
	isHost := *host

	if isHost {
		email := prompt(stdin, "email: ")
		password := prompt(stdin, "password (12+ chars): ")
		name := prompt(stdin, "display name: ")
		buyInStr := prompt(stdin, "buy-in (100-10000): ")
		buyIn, err := strconv.ParseInt(strings.TrimSpace(buyInStr), 10, 64)
		must(err)

		var reg struct {
			Token string `json:"token"`
		}
		postJSON(base+"/api/v1/auth/register", "", map[string]string{
			"email": email, "password": password, "display_name": name,
		}, &reg)

		var created struct {
			RoomID string `json:"room_id"`
			Code   string `json:"code"`
			Token  string `json:"token"`
		}
		postJSON(base+"/api/v1/rooms", reg.Token, map[string]int64{"buy_in": buyIn}, &created)

		fmt.Printf("Room created — code: %s\n", created.Code)
		token, roomID = created.Token, created.RoomID
	} else {
		name := prompt(stdin, "display name: ")
		var joined struct {
			RoomID string `json:"room_id"`
			Token  string `json:"token"`
		}
		postJSON(base+"/api/v1/rooms/"+*join+"/participants", "", map[string]string{
			"display_name": name,
		}, &joined)
		token, roomID = joined.Token, joined.RoomID
	}

	fmt.Printf("Connected to room %s\n", roomID)

	wsURL := "ws://" + *addr + "/api/v1/socket?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	must(err)
	defer conn.Close()

	go readLoop(conn)

	if isHost {
		fmt.Println("Commands: round <question> | <outcome> | <outcome> [| <outcome>...]    resolve <outcome-index>")
	} else {
		fmt.Println("Commands: bet <outcome-index> <amount>")
	}

	for {
		line, err := stdin.ReadString('\n')
		if err != nil {
			return
		}
		if err := handleCommand(conn, strings.TrimSpace(line)); err != nil {
			fmt.Println("error:", err)
		}
	}
}

func handleCommand(conn *websocket.Conn, line string) error {
	if line == "" {
		return nil
	}
	fields := strings.SplitN(line, " ", 2)
	switch fields[0] {
	case "round":
		if len(fields) < 2 {
			return fmt.Errorf("usage: round <question> | <outcome> | <outcome>")
		}
		parts := strings.Split(fields[1], "|")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		if len(parts) < 3 {
			return fmt.Errorf("need a question and at least 2 outcomes, separated by |")
		}
		return sendEnvelope(conn, "create_round", map[string]any{
			"question": parts[0], "outcomes": parts[1:], "lock_in_ms": 30000,
		})

	case "bet":
		parts := strings.Fields(fields[1])
		if len(parts) != 2 {
			return fmt.Errorf("usage: bet <outcome-index> <amount>")
		}
		outcome, err := strconv.Atoi(parts[0])
		if err != nil {
			return err
		}
		amount, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return err
		}
		return sendEnvelope(conn, "place_wager", map[string]any{
			"outcome": outcome, "amount": amount, "idempotency_key": uuid.NewString(),
		})

	case "resolve":
		outcome, err := strconv.Atoi(strings.TrimSpace(fields[len(fields)-1]))
		if err != nil {
			return err
		}
		return sendEnvelope(conn, "resolve_round", map[string]any{"winning_outcome": outcome})

	default:
		return fmt.Errorf("unknown command %q", fields[0])
	}
}

// envelope mirrors internal/ws.Envelope's wire format — this binary
// deliberately has no import on the backend's internal packages, same
// as any other client of the public socket protocol.
type envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func sendEnvelope(conn *websocket.Conn, msgType string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(envelope{Type: msgType, Data: raw})
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, payload)
}

func readLoop(conn *websocket.Conn) {
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			fmt.Println("connection closed:", err)
			return
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			fmt.Println("malformed message:", err)
			continue
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, env.Data, "", "  "); err == nil {
			fmt.Printf("<< %s %s\n", env.Type, pretty.String())
		} else {
			fmt.Printf("<< %s %s\n", env.Type, env.Data)
		}
	}
}

func postJSON(url, token string, body, out any) {
	raw, err := json.Marshal(body)
	must(err)
	req, err := http.NewRequest("POST", url, bytes.NewReader(raw))
	must(err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	must(err)
	defer resp.Body.Close()

	var env struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	must(json.NewDecoder(resp.Body).Decode(&env))
	if resp.StatusCode >= 300 {
		msg := "request failed"
		if env.Error != nil {
			msg = env.Error.Code + ": " + env.Error.Message
		}
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(1)
	}
	must(json.Unmarshal(env.Data, out))
}

func prompt(r *bufio.Reader, label string) string {
	fmt.Print(label)
	line, err := r.ReadString('\n')
	must(err)
	return strings.TrimSpace(line)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
