-- Documents are a collection (see package content). A document has a media
-- file or an external link. downloads is counted by the module itself and is
-- not one of the collection's fields, so saving a document keeps it.
CREATE TABLE wt_document (
    id            TEXT PRIMARY KEY,
    instance      TEXT NOT NULL,
    published     BOOLEAN NOT NULL DEFAULT FALSE,
    position      INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMP NOT NULL,
    created_by    TEXT,
    updated_at    TIMESTAMP NOT NULL,
    updated_by    TEXT,
    deleted_at    TIMESTAMP,
    deleted_by    TEXT,
    file_id       TEXT,
    link          TEXT NOT NULL DEFAULT '',
    document_date TIMESTAMP NOT NULL,
    downloads     INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX wt_document_instance ON wt_document (instance, position);

CREATE TABLE wt_document_translation (
    item_id     TEXT NOT NULL REFERENCES wt_document (id) ON DELETE CASCADE,
    locale      TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
