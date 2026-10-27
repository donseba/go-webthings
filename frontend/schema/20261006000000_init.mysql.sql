-- Visitors' posts with mistakes (Frontend.KeepForm), each waiting at most
-- five minutes for the first view of its form's page, which removes it. id
-- is the SHA-256 of the token in the visitor's cookie, never the token
-- itself; client is an HMAC of the visitor's address, which limits the
-- posts kept per visitor. payload is go-form's Carry.
CREATE TABLE wt_kept_form (
    id         VARCHAR(64) PRIMARY KEY,
    client     VARCHAR(64) NOT NULL,
    form_key   VARCHAR(64) NOT NULL,
    page       VARCHAR(2048) NOT NULL,
    action     VARCHAR(2048) NOT NULL,
    payload    MEDIUMTEXT NOT NULL,
    created_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    INDEX wt_kept_form_client (client, created_at),
    INDEX wt_kept_form_expires (expires_at)
) ENGINE=InnoDB;
