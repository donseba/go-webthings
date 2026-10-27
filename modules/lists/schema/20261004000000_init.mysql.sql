-- One row per list entry; the instance is the list (see content.Collection).
CREATE TABLE wt_lists_entry (
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
    image_id   VARCHAR(36) NULL,
    link_url   VARCHAR(2000) NOT NULL DEFAULT '',
    INDEX wt_lists_entry_instance (instance, position)
) ENGINE=InnoDB;

CREATE TABLE wt_lists_entry_translation (
    item_id VARCHAR(36) NOT NULL,
    locale  VARCHAR(35) NOT NULL,
    title   VARCHAR(200) NOT NULL DEFAULT '',
    body    MEDIUMTEXT NOT NULL,
    PRIMARY KEY (item_id, locale),
    FOREIGN KEY (item_id) REFERENCES wt_lists_entry (id) ON DELETE CASCADE
) ENGINE=InnoDB;
