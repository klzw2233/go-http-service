-- users 表。
--
-- 唯一约束建在 lower(username) 而不是 username 上：Jimmy 和 jimmy 应当
-- 视为同一个人，但用户输入的大小写要保留下来用于显示。代价是查询必须写成
-- WHERE lower(username) = lower($1) 才能命中索引。
--
-- 邮箱同理。RFC 上邮箱的 local part 理论上区分大小写，但实践中没有任何
-- 主流邮件服务这么做，按不区分处理可以避免同一个人注册两次。

CREATE TABLE users (
    id            BIGSERIAL   PRIMARY KEY,
    username      TEXT        NOT NULL,
    email         TEXT        NOT NULL,
    -- bcrypt 输出固定 60 字符，留 TEXT 是为了将来换算法时不必改表
    password_hash TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 索引名会被 repository 层用来区分冲突类型，改名要同步改代码
CREATE UNIQUE INDEX users_username_key ON users (lower(username));
CREATE UNIQUE INDEX users_email_key    ON users (lower(email));
