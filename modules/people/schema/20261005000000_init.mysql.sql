-- People are a collection (see package content). The slug is the same in every
-- language and unique per instance.
CREATE TABLE wt_person (
    id         VARCHAR(36) PRIMARY KEY,
    instance   VARCHAR(64) NOT NULL,
    published  BOOLEAN NOT NULL DEFAULT FALSE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL,
    created_by VARCHAR(36) NULL,
    updated_at DATETIME(6) NOT NULL,
    updated_by VARCHAR(36) NULL,
    deleted_at DATETIME(6) NULL,
    deleted_by VARCHAR(36) NULL,
    slug       VARCHAR(190) NOT NULL,
    -- Deleted rows keep no slug: MySQL has no partial indexes, but allows
    -- many NULLs in a unique one.
    live_slug  VARCHAR(190) AS (CASE WHEN deleted_at IS NULL THEN slug END) STORED,
    name       VARCHAR(200) NOT NULL DEFAULT '',
    photo_id   VARCHAR(36) NULL,
    email      VARCHAR(254) NOT NULL DEFAULT '',
    phone      VARCHAR(50) NOT NULL DEFAULT '',
    website    VARCHAR(500) NOT NULL DEFAULT '',
    linkedin   VARCHAR(500) NOT NULL DEFAULT '',
    INDEX wt_person_instance (instance, position),
    UNIQUE INDEX wt_person_slug (instance, live_slug)
) ENGINE=InnoDB;

CREATE TABLE wt_person_translation (
    item_id    VARCHAR(36) NOT NULL,
    locale     VARCHAR(35) NOT NULL,
    group_name VARCHAR(200) NOT NULL DEFAULT '',
    role_title VARCHAR(200) NOT NULL DEFAULT '',
    bio        MEDIUMTEXT NOT NULL,
    PRIMARY KEY (item_id, locale),
    FOREIGN KEY (item_id) REFERENCES wt_person (id) ON DELETE CASCADE
) ENGINE=InnoDB;
