"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { openRoomSocket } from "@/lib/socket";
import type { SocketStatus } from "@/lib/socket";
import { getRoomSummary, getRoomToken } from "@/lib/session";
import type { ConnectedEvent, PresenceEvent } from "@/lib/protocol";
import { PresenceRoster } from "@/components/PresenceRoster";

type Player = { user_id: string; display_name: string };

export default function Page({
  params,
}: {
  params: Promise<{ code: string }>;
}) {
  void params; // the room's identity comes from the token, never the URL

  const [selfId, setSelfId] = useState<string | null>(null);
  const [players, setPlayers] = useState<Player[]>([]);
  const [playerCount, setPlayerCount] = useState(0);
  const [status, setStatus] = useState<SocketStatus>("connecting");

  const roomToken = getRoomToken();
  const summary = getRoomSummary();

  useEffect(() => {
    const token = getRoomToken();
    if (!token) {
      return;
    }

    const socket = openRoomSocket(token);

    const offStatus = socket.onStatus((s) => setStatus(s));

    const offConnected = socket.on("connected", (data) => {
      const event = data as ConnectedEvent;
      setSelfId(event.user_id);
      setPlayers([{ user_id: event.user_id, display_name: event.display_name }]);
      setPlayerCount(1);
    });

    const offJoined = socket.on("player_joined", (data) => {
      const event = data as PresenceEvent;
      setPlayers((prev) => [
        ...prev.filter((p) => p.user_id !== event.user_id),
        { user_id: event.user_id, display_name: event.display_name },
      ]);
      setPlayerCount(event.player_count);
    });

    const offLeft = socket.on("player_left", (data) => {
      const event = data as PresenceEvent;
      setPlayers((prev) => prev.filter((p) => p.user_id !== event.user_id));
      setPlayerCount(event.player_count);
    });

    return () => {
      offStatus();
      offConnected();
      offJoined();
      offLeft();
      socket.close();
    };
  }, []);

  if (!roomToken) {
    return (
      <main className="flex flex-1 flex-col items-center justify-center gap-4 px-4 py-16">
        <p>Join a room first.</p>
        <Link href="/" className="text-sm underline">
          Back to join a room
        </Link>
      </main>
    );
  }

  return (
    <main className="flex flex-1 flex-col items-center gap-6 px-4 py-16">
      {summary && (
        <>
          <p className="text-2xl font-semibold">{summary.session_balance}</p>
          {summary.partial_buy_in && (
            <p>
              Joined with a partial buy-in: {summary.session_balance} tokens
            </p>
          )}
        </>
      )}

      <p>{status}</p>
      <p>{playerCount}</p>

      <PresenceRoster players={players} selfId={selfId} />
    </main>
  );
}
