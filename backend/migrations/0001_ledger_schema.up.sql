-- Editing this migration in place (rather than adding 0002, 0003, ...) is
-- correct only here: as of Phase 5a, 0001 has never been applied outside a
-- scratch database that internal/migrate's TestMain drops and recreates on
-- every run, so there is no deployed state to preserve. The
-- database-migrations skill's rule against editing an applied migration
-- takes effect the moment this runs anywhere else — Phase 5b onward.

CREATE TABLE accounts (
    id         uuid PRIMARY KEY,
    kind       text NOT NULL CHECK (kind IN ('user_wallet', 'room_escrow', 'round_pool', 'system_mint', 'system_dust')),
    user_id    uuid,
    room_id    uuid,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE transactions (
    id              uuid PRIMARY KEY,
    idempotency_key text NOT NULL,
    kind            text NOT NULL,
    room_id         uuid,
    round_id        uuid,
    occurred_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE ledger_entries (
    id             uuid PRIMARY KEY,
    transaction_id uuid NOT NULL REFERENCES transactions (id),
    account_id     uuid NOT NULL REFERENCES accounts (id),
    direction      text NOT NULL CHECK (direction IN ('debit', 'credit')),
    amount         bigint NOT NULL
);
