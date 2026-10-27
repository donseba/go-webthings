-- Documents are a collection (see package content). A document has a media
-- file or an external link. downloads is counted by the module itself and is
-- not one of the collection's fields, so saving a document keeps it.
CREATE TABLE wt_document (
    id            UUID PRIMARY KEY,
    instance      VARCHAR(64) NOT NULL,
    published     BOOLEAN NOT NULL DEFAULT FALSE,
    position      INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL,
    created_by    UUID,
    updated_at    TIMESTAMPTZ NOT NULL,
    updated_by    UUID,
    deleted_at    TIMESTAMPTZ,
    deleted_by    UUID,
    file_id       UUID,
    link          VARCHAR(2000) NOT NULL DEFAULT '',
    document_date TIMESTAMPTZ NOT NULL,
    downloads     INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX wt_document_instance ON wt_document (instance, position);

CREATE TABLE wt_document_translation (
    item_id     UUID NOT NULL REFERENCES wt_document (id) ON DELETE CASCADE,
    locale      VARCHAR(35) NOT NULL,
    title       VARCHAR(200) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    category    VARCHAR(200) NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, locale)
);
