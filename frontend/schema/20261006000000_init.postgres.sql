-- Visitors' posts with mistakes (Frontend.KeepForm), each waiting at most
-- five minutes for the first view of its form's page, which removes it. id
-- is the SHA-256 of the token in the visitor's cookie, never the token
-- itself; client is an HMAC of the visitor's address, which limits the
-- posts kept per visitor. payload is go-form's Carry.
CREATE TABLE wt_kept_form (
    id         VARCHAR(64) PRIMARY KEY,
    client     VARCHAR(64) NOT NULL,
    form_key   VARCHAR(64) NOT NULL,
    page       TEXT NOT NULL,
    action     TEXT NOT NULL,
    payload    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX wt_kept_form_client ON wt_kept_form (client, created_at);

CREATE INDEX wt_kept_form_expires ON wt_kept_form (expires_at);
