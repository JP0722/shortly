-- 0001_links.sql — the links table.

CREATE TABLE IF NOT EXISTS links (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code       VARCHAR(16)     NOT NULL,
    target_url VARCHAR(2048)   NOT NULL,
    created_at TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (id),

    -- The real defence against short-code collisions.
    --
    -- Checking "does this code exist?" and then inserting is a race: two
    -- requests can both check, both see nothing, and both insert. A unique
    -- index makes MySQL reject the second one no matter how the timing falls,
    -- and the application retries with a fresh code.
    UNIQUE KEY uq_links_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
