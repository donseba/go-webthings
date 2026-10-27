-- Two-step sign-in: the secret an authenticator app shares with the
-- account, encrypted with a key of the site (AES-GCM). enabled_at is empty
-- while a setup waits for its first code; last_step is the last 30-second
-- step whose code was used, so no code works twice.
CREATE TABLE wt_two_step (
    user_id    TEXT PRIMARY KEY REFERENCES wt_users (id) ON DELETE CASCADE,
    secret     TEXT NOT NULL,
    last_step  INTEGER NOT NULL DEFAULT 0,
    enabled_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL
);

-- Recovery codes work once each: only SHA-256 digests of them are stored.
CREATE TABLE wt_recovery_codes (
    user_id    TEXT NOT NULL REFERENCES wt_users (id) ON DELETE CASCADE,
    code_hash  TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    PRIMARY KEY (user_id, code_hash)
);

-- Sign-ins whose password was right and that wait for a code: only digests
-- of their tokens are stored.
CREATE TABLE wt_pending_sign_ins (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES wt_users (id) ON DELETE CASCADE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX wt_pending_sign_ins_user ON wt_pending_sign_ins (user_id);
