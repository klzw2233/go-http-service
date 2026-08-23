-- refresh_tokens 表。
--
-- 存的是 token 的 SHA-256 十六进制，不是 token 本身。refresh token 和密码
-- 一样是凭据，库泄露就会导致账号沦陷，所以不能明文存。
--
-- 但不能用 bcrypt：refresh token 是 32 字节随机值，不存在「猜出原文」的问题，
-- 慢哈希毫无收益，而每次刷新都要哈希一次，默认成本 ~60ms 会让这个接口变成
-- CPU 瓶颈。SHA-256 对高熵输入足够，且每次刷新只是一次哈希。
--
-- ON DELETE CASCADE：删用户时自动清掉他的会话。

CREATE TABLE refresh_tokens (
    id         BIGSERIAL   PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- SHA-256 hex of the raw token; UNIQUE so a lookup is a single row.
    token_hash TEXT        NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX refresh_tokens_user_id_idx ON refresh_tokens (user_id);
