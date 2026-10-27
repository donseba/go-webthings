-- Redirects send an old path to a new address. from_path is normalised (see
-- package redirects); to_url is a path on the site or an absolute URL. Paths
-- compare case-sensitively, like the rest of the site.
CREATE TABLE wt_redirect (
    id          VARCHAR(36) PRIMARY KEY,
    instance    VARCHAR(64) NOT NULL,
    from_path   VARCHAR(500) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    to_url      VARCHAR(2000) NOT NULL,
    status      INTEGER NOT NULL DEFAULT 301,
    hits        BIGINT NOT NULL DEFAULT 0,
    last_hit_at DATETIME(6) NULL,
    created_at  DATETIME(6) NOT NULL,
    created_by  VARCHAR(36) NULL,
    updated_at  DATETIME(6) NOT NULL,
    updated_by  VARCHAR(36) NULL,
    deleted_at  DATETIME(6) NULL,
    deleted_by  VARCHAR(36) NULL,
    -- Redirects in the trash keep their path, but leave it free for a new
    -- one: MySQL has no partial indexes, but allows many NULLs in a unique
    -- one.
    live_from   VARCHAR(500) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin AS (CASE WHEN deleted_at IS NULL THEN from_path END) STORED,
    UNIQUE KEY wt_redirect_from (instance, live_from)
) ENGINE=InnoDB;

-- Missed paths are public GET requests that ended in 404, counted so editors
-- can add redirects for them.
CREATE TABLE wt_redirect_miss (
    instance    VARCHAR(64) NOT NULL,
    path        VARCHAR(500) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    hits        BIGINT NOT NULL DEFAULT 0,
    last_hit_at DATETIME(6) NOT NULL,
    PRIMARY KEY (instance, path)
) ENGINE=InnoDB;
