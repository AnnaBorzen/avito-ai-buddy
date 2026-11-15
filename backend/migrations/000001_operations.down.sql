-- Rollback is destructive and intended for development only.
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS tickets;
DROP TABLE IF EXISTS operation_payloads;
DROP TABLE IF EXISTS operations;
DROP FUNCTION IF EXISTS set_updated_at();
DROP SEQUENCE IF EXISTS operation_id_seq;
