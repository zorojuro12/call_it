"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { openRoomSocket } from "@/lib/socket";
import type { SocketStatus } from "@/lib/socket";
import { getRoomSummary, getRoomToken } from "@/lib/session";
import type { ConnectedEvent } from "@/lib/protocol";

export default function Page({
  params,
}: {
  params: Promise<{ code: string }>;
}) {
  void params; // the room's identity comes from the token, never the URL

  const [displayName, setDisplayName] = useState<string | null>(null);
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
      setDisplayName(event.display_name);
    });

    return () => {
      offStatus();
      offConnected();
      socket.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
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
      {displayName && <p>{displayName}</p>}

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
    </main>
  );
}
