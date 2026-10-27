-- Items an administrator marked permanent (see admin.Permanence): they stay
-- on the website, so their editor refuses to unpublish them, to plan their
-- unpublication and to delete them. section is the item's admin section,
-- such as "pages" or "news". Removing the mark keeps the row with permanent
-- false, so it records who removed it.
CREATE TABLE wt_permanent_items (
    section    VARCHAR(100) NOT NULL,
    item_id    VARCHAR(100) NOT NULL,
    permanent  BOOLEAN NOT NULL,
    created_at DATETIME(6) NOT NULL,
    created_by VARCHAR(36) NULL,
    updated_at DATETIME(6) NOT NULL,
    updated_by VARCHAR(36) NULL,
    PRIMARY KEY (section, item_id)
) ENGINE=InnoDB;
