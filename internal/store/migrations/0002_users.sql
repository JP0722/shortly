-- 0002_users.sql — user accounts.

CREATE TABLE IF NOT EXISTS users (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    email         VARCHAR(320)    NOT NULL,

    -- The full PHC string, e.g.
    --   $argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>
    -- Parameters live inside it, so raising the cost later does not break
    -- existing users: each hash is verified with the parameters it was made with.
    password_hash VARCHAR(255)    NOT NULL,

    role          VARCHAR(16)     NOT NULL DEFAULT 'user',
    created_at    TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (id),

    -- Same reasoning as links.code: this is the real guard against two
    -- registrations for one email racing each other, not an application check.
    UNIQUE KEY uq_users_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
