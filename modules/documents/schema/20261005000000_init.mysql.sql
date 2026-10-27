-- Documents are a collection (see package content). A document has a media
-- file or an external link. downloads is counted by the module itself and is
-- not one of the collection's fields, so saving a document keeps it.
CREATE TABLE wt_document (
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
    file_id       VARCHAR(36) NULL,
    link          VARCHAR(2000) NOT NULL DEFAULT '',
    document_date DATETIME(6) NOT NULL,
    downloads     INTEGER NOT NULL DEFAULT 0,
    INDEX wt_document_instance (instance, position)
) ENGINE=InnoDB;

CREATE TABLE wt_document_translation (
    item_id     VARCHAR(36) NOT NULL,
    locale      VARCHAR(35) NOT NULL,
    title       VARCHAR(200) NOT NULL DEFAULT '',
    description MEDIUMTEXT NOT NULL,
    category    VARCHAR(200) NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale),
    FOREIGN KEY (item_id) REFERENCES wt_document (id) ON DELETE CASCADE
) ENGINE=InnoDB;
