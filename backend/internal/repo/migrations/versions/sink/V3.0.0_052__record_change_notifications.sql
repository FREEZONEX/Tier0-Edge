-- records:subscribe 的事件源（A6）：与记录写入同一事务插入一行，只存标识不存业务数据。
-- xid 是写入事务号；读取方只读 xid 小于当前快照 xmin 的行（这些事务都已结束），按 (xid, seq) 递增续传，不会漏掉晚提交的事务。
-- Enterprise currently grants complete Collections; events contain identities only.
CREATE TABLE IF NOT EXISTS public.uns_record_change (
    seq           BIGSERIAL PRIMARY KEY,
    xid           xid8   NOT NULL DEFAULT pg_current_xact_id(),
    collection_id TEXT   NOT NULL,
    record_id     TEXT   NOT NULL,
    revision      BIGINT NOT NULL,
    change        TEXT   NOT NULL CHECK (change IN ('record', 'delete')),
    created_at    BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_uns_record_change_position ON public.uns_record_change (xid, seq);
CREATE INDEX IF NOT EXISTS idx_uns_record_change_created ON public.uns_record_change (created_at);
-- 已清理下界：清理删掉的最大 (xid, seq)。游标位置早于它时可能漏掉了被删的行，按 CURSOR_EXPIRED 处理（与签发时间、时钟无关）。
CREATE TABLE IF NOT EXISTS public.uns_record_change_purged (
    id  SMALLINT PRIMARY KEY CHECK (id = 1),
    xid xid8   NOT NULL,
    seq BIGINT NOT NULL
);

-- Cover every local writer in the same transaction, including Source/Stream and native imports.
-- Lease-only updates, no-op writes, Sample-only writes and permanent garbage collection do not emit changes.
CREATE FUNCTION public.capture_uns_record_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND OLD.data IS NOT DISTINCT FROM NEW.data AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at THEN RETURN NEW; END IF;
 INSERT INTO public.uns_record_change(collection_id,record_id,revision,change,created_at)
 VALUES(NEW.collection_id,NEW.id,NEW.revision,CASE WHEN NEW.deleted_at IS NULL THEN 'record' ELSE 'delete' END,(extract(epoch FROM clock_timestamp())*1000)::bigint);
 RETURN NEW;
END $$;
CREATE TRIGGER uns_record_change_capture AFTER INSERT OR UPDATE OF data,deleted_at ON public.uns_record
 FOR EACH ROW EXECUTE FUNCTION public.capture_uns_record_change();
