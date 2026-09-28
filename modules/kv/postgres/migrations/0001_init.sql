CREATE TABLE kv (
    key        text PRIMARY KEY,
    value      bytea NOT NULL,
    expires_at timestamptz
);
CREATE INDEX kv_expires ON kv (expires_at) WHERE expires_at IS NOT NULL;
