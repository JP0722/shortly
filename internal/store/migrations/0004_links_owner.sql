-- 0004_links_owner.sql — links now belong to a user.
--
-- user_id is NULLABLE on purpose. Rows already exist from Phase 1 with no owner,
-- and a NOT NULL column with no default cannot be added to a non-empty table.
-- Every migration has to work against the data that is already there.

ALTER TABLE links
    ADD COLUMN user_id BIGINT UNSIGNED NULL DEFAULT NULL AFTER id;

ALTER TABLE links
    ADD KEY idx_links_user (user_id, id);

ALTER TABLE links
    ADD CONSTRAINT fk_links_user FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE;
