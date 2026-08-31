// Shared k6 helpers: register a user, create a room, join by code.
// Route paths, request field names, and the envelope shape are read
// directly from backend/internal/httpapi/auth_handlers.go,
// room_handlers.go, and respond.go — not guessed.

import http from 'k6/http';
import { check } from 'k6';

export const BASE = __ENV.API_BASE_URL || 'http://localhost:8080';

// unwrapData parses httpapi.WriteData's {"data": ...} success envelope
// and returns the inner value.
function unwrapData(res) {
  const body = JSON.parse(res.body);
  return body.data;
}

// registerUser registers a unique user and returns {token, userId}.
// Email is unique per VU/iteration/timestamp so a repeated scenario run
// never collides with account.ErrEmailTaken.
export function registerUser() {
  const email = `vu-${__VU}-${__ITER}-${Date.now()}@loadtest.local`;
  const res = http.post(
    `${BASE}/api/v1/auth/register`,
    JSON.stringify({
      email,
      password: 'correct horse battery staple',
      display_name: `LoadTest ${__VU}-${__ITER}`,
    }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  check(res, { 'register: status 201': (r) => r.status === 201 });
  const data = unwrapData(res);
  return { token: data.token, userId: data.account.id };
}

// createRoom creates a room as the authenticated caller identified by
// token, and returns {roomId, code, roomToken}. roomToken is the
// room-scoped token issued to the host — the one a socket connection
// authenticates with, distinct from the account token.
export function createRoom(token) {
  const res = http.post(
    `${BASE}/api/v1/rooms`,
    JSON.stringify({ buy_in: 1000 }),
    {
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
    }
  );
  check(res, { 'createRoom: status 201': (r) => r.status === 201 });
  const data = unwrapData(res);
  return { roomId: data.room_id, code: data.code, roomToken: data.token };
}

// joinRoom joins roomCode as a guest (no Authorization header — guests
// are supported by OptionalAuth) and returns {roomToken}, the
// room-scoped token this player's socket connection authenticates with.
export function joinRoom(code, displayName) {
  const res = http.post(
    `${BASE}/api/v1/rooms/${code}/participants`,
    JSON.stringify({ display_name: displayName }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  check(res, { 'joinRoom: status 201': (r) => r.status === 201 });
  const data = unwrapData(res);
  return { roomToken: data.token };
}
