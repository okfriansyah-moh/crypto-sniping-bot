-- Phase 9 additive: add symbol and name columns to market_data for Solana tokens.
-- Solana ingestion populates these from Pump.fun create and Raydium Initialize2 events.
-- EVM events leave them as empty string (DEFAULT '').
-- Never modify this file once committed — add a new migration instead.

BEGIN;

ALTER TABLE market_data ADD COLUMN IF NOT EXISTS symbol TEXT NOT NULL DEFAULT '';
ALTER TABLE market_data ADD COLUMN IF NOT EXISTS name   TEXT NOT NULL DEFAULT '';

COMMIT;
