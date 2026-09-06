-- Account identity. Phase 5b derives accounts.id deterministically from
-- (kind, user_id/room_id) so provisioning is a single ON CONFLICT (id) DO
-- NOTHING with no lookup round trip. These partial unique indexes are the
-- enforcing constraint behind that convention: a drifted or hand-written id
-- for an identity that already exists is rejected rather than silently
-- creating a second account that splits one holder's balance in two.
CREATE UNIQUE INDEX accounts_user_wallet_key
    ON accounts (user_id) WHERE kind = 'user_wallet';
CREATE UNIQUE INDEX accounts_round_pool_key
    ON accounts (room_id) WHERE kind = 'round_pool';
CREATE UNIQUE INDEX accounts_system_singleton_key
    ON accounts (kind) WHERE kind IN ('system_mint', 'system_dust');

-- assert_transaction_balanced() runs per inserted row and looks entries up
-- by transaction_id. Without this index that lookup is a sequential scan of
-- the whole ledger_entries table on every entry insert — quadratic over a
-- load test of thousands of wagers, which is precisely the run the flagship
-- reconciliation test performs.
CREATE INDEX ledger_entries_transaction_id_idx ON ledger_entries (transaction_id);

-- Balance queries aggregate a single account's entries.
CREATE INDEX ledger_entries_account_id_idx ON ledger_entries (account_id);
