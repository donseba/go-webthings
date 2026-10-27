-- Two-step sign-in: the secret an authenticator app shares with the
-- account, encrypted with a key of the site (AES-GCM). enabled_at is empty
-- while a setup waits for its first code; last_step is the last 30-second
-- step whose code was used, so no code works twice.
CREATE TABLE wt_two_step (
    user_id    VARCHAR(36) PRIMARY KEY,
    secret     VARCHAR(200) NOT NULL,
    last_step  BIGINT NOT NULL DEFAULT 0,
    enabled_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL,
    FOREIGN KEY (user_id) REFERENCES wt_users (id) ON DELETE CASCADE
) ENGINE=InnoDB;

-- Recovery codes work once each: only SHA-256 digests of them are stored.
CREATE TABLE wt_recovery_codes (
    user_id    VARCHAR(36) NOT NULL,
    code_hash  VARCHAR(64) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    PRIMARY KEY (user_id, code_hash),
    FOREIGN KEY (user_id) REFERENCES wt_users (id) ON DELETE CASCADE
) ENGINE=InnoDB;

-- Sign-ins whose password was right and that wait for a code: only digests
-- of their tokens are stored.
CREATE TABLE wt_pending_sign_ins (
    token_hash VARCHAR(64) PRIMARY KEY,
    user_id    VARCHAR(36) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    FOREIGN KEY (user_id) REFERENCES wt_users (id) ON DELETE CASCADE
) ENGINE=InnoDB;
