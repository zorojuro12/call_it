"use client";

import { useEffect, useReducer, useRef, useState } from "react";
import Link from "next/link";
import { openRoomSocket } from "@/lib/socket";
import type { RoomSocket, SocketStatus } from "@/lib/socket";
import { getRoomSummary, getRoomToken } from "@/lib/session";
import type { ConnectedEvent, PresenceEvent } from "@/lib/protocol";
import { PresenceRoster } from "@/components/PresenceRoster";
import { initialRoundState, reduceRound } from "@/lib/roundState";
import type { RoundAction } from "@/lib/roundState";
import { useCountdown } from "@/lib/countdown";
import { playCue } from "@/lib/audio";
import { OddsBoard } from "@/components/OddsBoard";
import { WagerPad } from "@/components/WagerPad";
import { HostConsole } from "@/components/HostConsole";
import { SettlementReveal } from "@/components/SettlementReveal";

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
  const socketRef = useRef<RoomSocket | null>(null);

  const roomToken = getRoomToken();
  const summary = getRoomSummary();

  const [round, dispatch] = useReducer(reduceRound, initialRoundState(summary?.session_balance ?? 0));
  const remainingMs = useCountdown(round.lock_at_ms);

  useEffect(() => {
    const token = getRoomToken();
    if (!token) {
      return;
    }

    const socket = openRoomSocket(token);
    socketRef.current = socket;

    const offStatus = socket.onStatus((s) => setStatus(s));

    const offConnected = socket.on("connected", (data) => {
      const event = data as ConnectedEvent;
      setSelfId(event.user_id);
      setPlayers([{ user_id: event.user_id, display_name: event.display_name }]);
      setPlayerCount(1);
      dispatch({ type: "connected", data: event });
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

    const offRoundOpened = socket.on("round_opened", (data) => {
      dispatch({ type: "round_opened", data } as RoundAction);
      playCue("open");
    });

    const offOddsUpdated = socket.on("odds_updated", (data) => {
      dispatch({ type: "odds_updated", data } as RoundAction);
    });

    const offRoundLocked = socket.on("round_locked", (data) => {
      dispatch({ type: "round_locked", data } as RoundAction);
      playCue("lock");
    });

    const offWagerAccepted = socket.on("wager_accepted", (data) => {
      dispatch({ type: "wager_accepted", data } as RoundAction);
    });

    const offRoundResolved = socket.on("round_resolved", (data) => {
      dispatch({ type: "round_resolved", data } as RoundAction);
      playCue("resolve");
    });

    const offRoundRefunded = socket.on("round_refunded", (data) => {
      dispatch({ type: "round_refunded", data } as RoundAction);
      playCue("resolve");
    });

    return () => {
      offStatus();
      offConnected();
      offJoined();
      offLeft();
      offRoundOpened();
      offOddsUpdated();
      offRoundLocked();
      offWagerAccepted();
      offRoundResolved();
      offRoundRefunded();
      socket.close();
      socketRef.current = null;
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

  const handleOpenRound = (question: string, outcomes: string[], lockInMs: number) => {
    socketRef.current?.send("create_round", { question, outcomes, lock_in_ms: lockInMs });
  };

  const handleResolve = (winningOutcome: number) => {
    socketRef.current?.send("resolve_round", { winning_outcome: winningOutcome });
  };

  const handlePlace = (outcome: number, amount: number) => {
    socketRef.current?.send("place_wager", {
      outcome,
      amount,
      idempotency_key: crypto.randomUUID(),
    });
  };

  return (
    <main className="flex flex-1 flex-col items-center gap-6 px-4 py-16">
      {summary && (
        <>
          <p className="text-2xl font-semibold">{round.balance}</p>
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

      {round.phase !== "idle" && round.question && <h1>{round.question}</h1>}

      {round.phase === "open" && remainingMs > 0 && <p>Lockout in {Math.ceil(remainingMs / 1000)}s</p>}

      {round.phase !== "idle" && (
        <OddsBoard
          outcomes={round.outcomes}
          pools={round.pools}
          total={round.total}
          multipliers={round.multipliers}
          bettors={round.bettors}
          players={round.players}
        />
      )}

      {round.is_host ? (
        <HostConsole
          phase={round.phase}
          outcomes={round.outcomes}
          onOpenRound={handleOpenRound}
          onResolve={handleResolve}
        />
      ) : (
        round.phase !== "idle" &&
        round.phase !== "revealed" && (
          <WagerPad
            outcomes={round.outcomes}
            balance={round.balance}
            disabled={round.phase !== "open"}
            onPlace={handlePlace}
          />
        )
      )}

      <SettlementReveal
        results={round.results}
        outcomes={round.outcomes}
        winningOutcome={round.winning_outcome}
        dust={round.dust}
        refunded={round.refunded}
        refundTotal={round.refund_total}
        selfId={selfId}
      />
    </main>
  );
}
