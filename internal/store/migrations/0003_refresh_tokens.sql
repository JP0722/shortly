-- 0003_refresh_tokens.sql — long-lived, revocable half of the auth pair.

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id    BIGINT UNSIGNED NOT NULL,

    -- All tokens descended from one login share a family. When we detect that a
    -- token has been used twice, we revoke the whole family: we cannot tell the
    -- thief from the victim, so both are logged out.
    family_id  CHAR(32)        NOT NULL,

    -- SHA-256 of the token, hex encoded. NOT argon2: this is 256 bits of
    -- randomness, not a human-chosen password, so there is nothing to brute
    -- force and no reason to pay argon2's 64 MiB on every refresh.
    token_hash CHAR(64)        NOT NULL,

    expires_at TIMESTAMP       NOT NULL,

    -- NULL until the token is exchanged. A non-NULL value arriving again is the
    -- reuse signal.
    used_at    TIMESTAMP       NULL DEFAULT NULL,
    revoked_at TIMESTAMP       NULL DEFAULT NULL,
    created_at TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    UNIQUE KEY uq_refresh_token_hash (token_hash),
    KEY idx_refresh_family (family_id),
    KEY idx_refresh_expires (expires_at),

    CONSTRAINT fk_refresh_user FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
