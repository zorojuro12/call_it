// WebSocket wager-latency scenario — measures spec §7's "global
// WebSocket sync latency < 30 ms" target via wager placement, the path
// that exercises both the wager write path (< 15 ms target) and the
// odds_updated broadcast fan-out in one round trip.
//
// Rate-limit pacing, deliberately: internal/wager's rate limiter
// (wager.Limit / wager.Window) allows 20 placements per 10s per user.
// This scenario paces each player VU strictly sequentially — send one
// place_wager, wait for its wager_accepted/error reply, then wait
// WAGER_INTERVAL_MS before the next — which caps every VU at roughly
// 1 wager/sec, comfortably under the 2/s the limiter allows. Scaling
// throughput is done by adding VUs (WAGER_PLAYERS), never by raising
// the per-VU rate.
//
// The host-cannot-wager invariant (CLAUDE.md) is structural here: the
// VU that creates the room (hostVU) only ever opens the round and never
// sends place_wager; every place_wager comes from a separate playerVU
// that joined as a guest.
//
// Each place_wager carries a fresh UUIDv4 idempotency_key — reusing one
// would measure the Lua script's dedupe/cache-hit path instead of a
// real placement.

import { WebSocket } from 'k6/websockets';
import { Trend } from 'k6/metrics';
import { registerUser, createRoom, joinRoom, BASE } from './lib/setup.js';

const WS_BASE = BASE.replace(/^http/, 'ws');

const wagerAckMs = new Trend('wager_ack_ms', true);

const TEST_DURATION_S = Number(__ENV.WAGER_TEST_DURATION_S || 30);
const WAGER_PLAYERS = Number(__ENV.WAGER_PLAYERS || 5);
const WAGER_INTERVAL_MS = 1000;
const SCENARIO_TIMEOUT_S = TEST_DURATION_S + 30;

export const options = {
  scenarios: {
    // Players connect first and wait for round_opened. round_opened is
    // broadcast once, at creation — a client connecting after that
    // broadcast has no way to learn the round is open (there is no
    // round-catchup message on connect, unlike player_joined's
    // newcomer backfill), so the host must be the *last* one to
    // connect, not the first.
    players: {
      executor: 'per-vu-iterations',
      vus: WAGER_PLAYERS,
      iterations: 1,
      exec: 'playerVU',
      maxDuration: `${SCENARIO_TIMEOUT_S}s`,
    },
    host: {
      executor: 'shared-iterations',
      vus: 1,
      iterations: 1,
      exec: 'hostVU',
      startTime: '5s', // let every player join and connect first
      maxDuration: `${SCENARIO_TIMEOUT_S}s`,
    },
  },
  thresholds: {
    wager_ack_ms: ['p(99)<15'],
  },
};

export function setup() {
  const host = registerUser();
  const room = createRoom(host.token);
  // A room ID, not per-user data — no player identity, stake, or wager
  // is on this line. Collected by Task 7's reconciliation gate to know
  // which room(s) to check after the run.
  console.log(`RECONCILE_ROOM_ID=${room.roomId}`);
  return { code: room.code, hostToken: room.roomToken };
}

// uuidv4 generates a RFC4122-shaped v4 UUID good enough for
// place_wager's idempotency_key — the server only checks the version
// nibble via google/uuid.Parse().Version(), which this satisfies.
function uuidv4() {
  const bytes = [];
  for (let i = 0; i < 16; i++) bytes.push(Math.floor(Math.random() * 256));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = bytes.map((b) => b.toString(16).padStart(2, '0'));
  return (
    `${hex[0]}${hex[1]}${hex[2]}${hex[3]}-${hex[4]}${hex[5]}-` +
    `${hex[6]}${hex[7]}-${hex[8]}${hex[9]}-` +
    `${hex[10]}${hex[11]}${hex[12]}${hex[13]}${hex[14]}${hex[15]}`
  );
}

// round.Service.Open (backend/internal/round/service.go) caps a round's
// lock window at MaxLockIn = 120s and rejects anything longer with
// ErrInvalidSpec — a single round cannot cover a scenario run longer
// than that. ROUND_LOCK_IN_MS stays comfortably under the cap regardless
// of TEST_DURATION_S.
const ROUND_LOCK_IN_MS = 90 * 1000;

// hostVU cycles rounds for the whole scenario: open, resolve as soon as
// it locks, open the next one — never wagering itself. playerVU does not
// track round identity; a place_wager between rounds gets no_active_round
// or pool_locked and retries a second later (its existing error-handling
// path), so it picks up each new round with no extra coordination.
export function hostVU(data) {
  return new Promise((resolve) => {
    const ws = new WebSocket(`${WS_BASE}/api/v1/socket?token=${data.hostToken}`);

    function openRound() {
      ws.send(
        JSON.stringify({
          type: 'create_round',
          data: {
            question: 'load test round',
            outcomes: ['Yes', 'No'],
            lock_in_ms: ROUND_LOCK_IN_MS,
          },
        })
      );
    }

    ws.onopen = () => openRound();

    ws.onmessage = (msg) => {
      let env;
      try {
        env = JSON.parse(msg.data);
      } catch (e) {
        return;
      }
      if (env.type === 'round_locked') {
        ws.send(
          JSON.stringify({ type: 'resolve_round', data: { winning_outcome: 0 } })
        );
        return;
      }
      if (env.type === 'round_resolved') {
        openRound();
      }
    };
    ws.onerror = () => resolve();
    ws.onclose = () => resolve();

    setTimeout(() => ws.close(), SCENARIO_TIMEOUT_S * 1000);
  });
}

// playerVU joins as a guest, waits for round_opened, then places wagers
// one at a time, recording each send-to-ack latency.
export function playerVU(data) {
  return new Promise((resolve) => {
    const player = joinRoom(data.code, `loadtest-p${__VU}`);
    const ws = new WebSocket(`${WS_BASE}/api/v1/socket?token=${player.roomToken}`);

    let roundOpen = false;
    let pendingSendMs = null;
    let endAt = null; // set once round_opened arrives — the wagering window starts there, not at connect

    function sendWager() {
      if (endAt !== null && Date.now() > endAt) {
        ws.close();
        return;
      }
      pendingSendMs = Date.now();
      ws.send(
        JSON.stringify({
          type: 'place_wager',
          data: {
            outcome: Math.random() < 0.5 ? 0 : 1,
            amount: 10,
            idempotency_key: uuidv4(),
          },
        })
      );
    }

    ws.onmessage = (msg) => {
      let env;
      try {
        env = JSON.parse(msg.data);
      } catch (e) {
        return;
      }

      if (env.type === 'round_opened' && !roundOpen) {
        roundOpen = true;
        endAt = Date.now() + TEST_DURATION_S * 1000;
        sendWager();
        return;
      }

      if (env.type === 'wager_accepted' && pendingSendMs !== null) {
        wagerAckMs.add(Date.now() - pendingSendMs);
        pendingSendMs = null;
        setTimeout(sendWager, WAGER_INTERVAL_MS);
        return;
      }

      // A rejected wager (e.g. rate_limited) doesn't count toward the
      // latency metric, but pacing must continue so the VU doesn't stall
      // for the rest of the run.
      if (env.type === 'error' && pendingSendMs !== null) {
        pendingSendMs = null;
        setTimeout(sendWager, WAGER_INTERVAL_MS);
      }
    };
    ws.onerror = () => resolve();
    ws.onclose = () => resolve();

    setTimeout(() => ws.close(), SCENARIO_TIMEOUT_S * 1000);
  });
}
