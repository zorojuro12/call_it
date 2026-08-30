"use client";

import Link from "next/link";

export default function Page() {
  return (
    <main className="flex flex-1 flex-col items-center justify-center gap-8 px-4 py-16">
      <h1 className="text-4xl font-semibold tracking-tight">CallIt</h1>

      <form className="flex w-full max-w-xs flex-col gap-4">
        <label className="flex flex-col gap-1 text-sm font-medium" htmlFor="room-code">
          Room code
          <input
            id="room-code"
            name="roomCode"
            type="text"
            autoComplete="off"
            className="rounded border border-zinc-300 px-3 py-2 uppercase dark:border-zinc-700 dark:bg-zinc-900"
          />
        </label>

        <button
          type="submit"
          className="rounded bg-zinc-950 px-4 py-2 font-medium text-white dark:bg-zinc-50 dark:text-black"
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
