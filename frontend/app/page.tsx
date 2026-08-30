"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { apiPost, ApiError } from "@/lib/api";
import { getAccountToken, setRoomSummary, setRoomToken } from "@/lib/session";
import type { JoinRoomResponse } from "@/lib/protocol";
import { ErrorBanner } from "@/components/ErrorBanner";

export default function Page() {
  const router = useRouter();
  const [roomCode, setRoomCode] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);

    const code = roomCode.trim().toUpperCase();

    try {
      const res = await apiPost<JoinRoomResponse>(
        `/api/v1/rooms/${encodeURIComponent(code)}/participants`,
        { display_name: displayName },
        getAccountToken() ?? undefined,
      );
      setRoomToken(res.token);
      setRoomSummary({
        room_id: res.room_id,
        guest: res.guest,
        session_balance: res.session_balance,
        partial_buy_in: res.partial_buy_in,
      });
      router.push(`/room/${code}`);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "could not join room");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="flex flex-1 flex-col items-center justify-center gap-8 px-4 py-16">
      <h1 className="text-4xl font-semibold tracking-tight">CallIt</h1>

      <form
        onSubmit={handleSubmit}
        className="flex w-full max-w-xs flex-col gap-4"
      >
        <label className="flex flex-col gap-1 text-sm font-medium" htmlFor="room-code">
          Room code
          <input
            id="room-code"
            name="roomCode"
            type="text"
            autoComplete="off"
            value={roomCode}
            onChange={(e) => setRoomCode(e.target.value)}
            className="rounded border border-zinc-300 px-3 py-2 uppercase dark:border-zinc-700 dark:bg-zinc-900"
          />
        </label>

        <label className="flex flex-col gap-1 text-sm font-medium" htmlFor="display-name">
          Display name
          <input
            id="display-name"
            name="displayName"
            type="text"
            autoComplete="nickname"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            className="rounded border border-zinc-300 px-3 py-2 dark:border-zinc-700 dark:bg-zinc-900"
          />
        </label>

        <ErrorBanner message={error} />

        <button
          type="submit"
          disabled={submitting}
          className="rounded bg-zinc-950 px-4 py-2 font-medium text-white disabled:opacity-50 dark:bg-zinc-50 dark:text-black"
        >
          Join
        </button>
      </form>

      <Link href="/login" className="text-sm underline">
        Log in
      </Link>
    </main>
  );
}
