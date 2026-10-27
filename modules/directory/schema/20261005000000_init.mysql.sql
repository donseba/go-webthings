-- Categories and entries are collections (see package content). Slugs are not
-- translated and are unique per instance.
CREATE TABLE wt_directory_category (
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
    UNIQUE INDEX wt_directory_category_slug (instance, live_slug)
) ENGINE=InnoDB;

CREATE TABLE wt_directory_category_translation (
    item_id VARCHAR(36) NOT NULL,
    locale  VARCHAR(35) NOT NULL,
    name    VARCHAR(200) NOT NULL DEFAULT '',
    summary TEXT NOT NULL,
    PRIMARY KEY (item_id, locale),
    FOREIGN KEY (item_id) REFERENCES wt_directory_category (id) ON DELETE CASCADE
) ENGINE=InnoDB;

-- A category with entries cannot be deleted: its entries would disappear from
-- the site. Editors move or delete them first.
CREATE TABLE wt_directory_entry (
    id            VARCHAR(36) PRIMARY KEY,
    instance      VARCHAR(64) NOT NULL,
    published     BOOLEAN NOT NULL DEFAULT FALSE,
    position      INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME(6) NOT NULL,
    created_by    VARCHAR(36) NULL,
    updated_at    DATETIME(6) NOT NULL,
    updated_by    VARCHAR(36) NULL,
    deleted_at    DATETIME(6) NULL,
    deleted_by    VARCHAR(36) NULL,
    category_id   VARCHAR(36) NULL,
    slug          VARCHAR(190) NOT NULL,
    -- Deleted rows keep no slug: MySQL has no partial indexes, but allows
    -- many NULLs in a unique one.
    live_slug     VARCHAR(190) AS (CASE WHEN deleted_at IS NULL THEN slug END) STORED,
    name          VARCHAR(200) NOT NULL DEFAULT '',
    image_id      VARCHAR(36) NULL,
    website       VARCHAR(500) NOT NULL DEFAULT '',
    email         VARCHAR(254) NOT NULL DEFAULT '',
    phone         VARCHAR(50) NOT NULL DEFAULT '',
    street        VARCHAR(200) NOT NULL DEFAULT '',
    postcode      VARCHAR(20) NOT NULL DEFAULT '',
    city          VARCHAR(100) NOT NULL DEFAULT '',
    latitude      DOUBLE NULL,
    longitude     DOUBLE NULL,
    opening_hours TEXT NOT NULL,
    UNIQUE INDEX wt_directory_entry_slug (instance, live_slug),
    INDEX wt_directory_entry_category (category_id),
    FOREIGN KEY (category_id) REFERENCES wt_directory_category (id)
) ENGINE=InnoDB;

CREATE TABLE wt_directory_entry_translation (
    item_id     VARCHAR(36) NOT NULL,
    locale      VARCHAR(35) NOT NULL,
    summary     TEXT NOT NULL,
    description MEDIUMTEXT NOT NULL,
    keywords    TEXT NOT NULL,
    hours_note  VARCHAR(500) NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale),
    FOREIGN KEY (item_id) REFERENCES wt_directory_entry (id) ON DELETE CASCADE
) ENGINE=InnoDB;
