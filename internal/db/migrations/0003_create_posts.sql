-- posts 表。
--
-- 单作者博客的内容表。id 是主键但不进公开 URL（公开 URL 是 slug）。
-- slug 全局唯一，含 Draft：未发布和已发布的 Post 共享同一命名空间，
-- 避免一个 Draft 占住某 slug 后，另一个已发布的 Post 还能用它。
--
-- published 默认 false：创建永远产出 Draft，没有「创建即发布」的捷径。
-- published_at 可空，首次 Publish 时写入，Unpublish 后保留不清（ADR-0005），
-- 便于公开首页按首次发布时间排序。这两个字段本迁移建好，但当前切片
-- （#3 Draft JSON）只读写 slug/title/body/published；published_at 留给 #4。
--
-- slug 用 TEXT 不用 lower()：ADR-0006 规定 slug 是作者选定的 ASCII 小写，
-- 形状校验在 service 层用正则兜住，库里不需要大小写折叠，唯一约束直接建在 slug 上。
-- 不加 version/乐观锁列：ADR-0015 last-save-wins，显式不做。

CREATE TABLE posts (
    id           BIGSERIAL    PRIMARY KEY,
    slug         TEXT         NOT NULL,
    title        TEXT         NOT NULL,
    body         TEXT         NOT NULL,
    published    BOOLEAN      NOT NULL DEFAULT false,
    published_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 索引名会被 repository 层用来区分 slug 唯一冲突，改名要同步改代码
CREATE UNIQUE INDEX posts_slug_key ON posts (slug);
