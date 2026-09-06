"use client";

import { useState } from "react";
import Link from "next/link";
import { apiPost, ApiError } from "@/lib/api";
import {
  getAccountToken,
  setRoomSummary,
  setRoomToken,
} from "@/lib/session";
import type { CreateRoomResponse } from "@/lib/protocol";
import { ErrorBanner } from "@/components/ErrorBanner";

const DEFAULT_BUY_IN = 1000;

export default function Page() {
  const accountToken = getAccountToken();
  const [buyIn, setBuyIn] = useState(String(DEFAULT_BUY_IN));
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [room, setRoom] = useState<CreateRoomResponse | null>(null);

  if (!accountToken) {
    return (
      <main className="flex flex-1 flex-col items-center justify-center gap-4 px-4 py-16">
        <p>You need an account to host a room.</p>
        <Link href="/login" className="text-sm underline">
          Log in
        </Link>
      </main>
    );
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);

    try {
      const res = await apiPost<CreateRoomResponse>(
        "/api/v1/rooms",
        { buy_in: Number(buyIn) },
        accountToken ?? undefined,
      );
      setRoomToken(res.token);
      setRoomSummary({
        room_id: res.room_id,
        guest: false,
        session_balance: res.buy_in,
        partial_buy_in: false,
      });
      setRoom(res);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "could not create room");
    } finally {
      setSubmitting(false);
    }
  }

  if (room) {
    const joinUrl =
      typeof window !== "undefined"
        ? `${window.location.origin}/room/${room.code}`
        : `/room/${room.code}`;

    return (
      <main className="flex flex-1 flex-col items-center justify-center gap-6 px-4 py-16">
        <h1 className="text-2xl font-semibold">Room created</h1>
        <p className="text-4xl font-bold tracking-widest">{room.code}</p>

        <label className="flex w-full max-w-xs flex-col gap-1 text-sm font-medium" htmlFor="share-link">
          Shareable link
          <input
            id="share-link"
            type="text"
            readOnly
            value={joinUrl}
            onFocus={(e) => e.currentTarget.select()}
            className="rounded border border-zinc-300 px-3 py-2 dark:border-zinc-700 dark:bg-zinc-900"
          />
        </label>

        <Link href={`/room/${room.code}`} className="text-sm underline">
          Go to room
        </Link>
      </main>
    );
  }

  return (
    <main className="flex flex-1 flex-col items-center justify-center gap-6 px-4 py-16">
      <h1 className="text-2xl font-semibold">Create a room</h1>

      <form
        onSubmit={handleSubmit}
        className="flex w-full max-w-xs flex-col gap-4"
      >
        <label className="flex flex-col gap-1 text-sm font-medium" htmlFor="buy-in">
          Buy-in
          <input
            id="buy-in"
            name="buyIn"
            type="number"
            min={1}
            value={buyIn}
            onChange={(e) => setBuyIn(e.target.value)}
            className="rounded border border-zinc-300 px-3 py-2 dark:border-zinc-700 dark:bg-zinc-900"
          />
        </label>

        <ErrorBanner message={error} />

        <button
          type="submit"
          disabled={submitting}
          className="rounded bg-zinc-950 px-4 py-2 font-medium text-white disabled:opacity-50 dark:bg-zinc-50 dark:text-black"
        >
          Create room
        </button>
      </form>
    </main>
  );
}
