package repo

import (
	"context"
	"math"
	"strconv"

	"gorm.io/gorm"
)

// RecordChange 是 uns_record_change 的一行（A6）。xid 以文本读出（xid_text），排序与比较始终用表里的 xid8 列。
type RecordChange struct {
	Seq          int64  `gorm:"column:seq"`
	XID          string `gorm:"column:xid_text"`
	CollectionID string `gorm:"column:collection_id"`
	RecordID     string `gorm:"column:record_id"`
	Revision     int64  `gorm:"column:revision"`
	Change       string `gorm:"column:change"`
	CreatedAt    int64  `gorm:"column:created_at"`
}

// RecordChangePosition 是续传位置：最后投递的 (xid, seq)。
type RecordChangePosition struct {
	XID string `gorm:"column:xid_text"`
	Seq int64  `gorm:"column:seq"`
}

// Less 按 (xid, seq) 数值比较
func (p RecordChangePosition) Less(other RecordChangePosition) bool {
	a, _ := strconv.ParseUint(p.XID, 10, 64)
	b, _ := strconv.ParseUint(other.XID, 10, 64)
	return a < b || a == b && p.Seq < other.Seq
}

// Max 返回两个位置中较大的一个
func (p RecordChangePosition) Max(other RecordChangePosition) RecordChangePosition {
	if p.Less(other) {
		return other
	}
	return p
}

// RecordChangesWindow 是一次读取所在快照的边界，必须在同一个只读事务里使用
type RecordChangesWindow struct {
	// Watermark 是可投递的 xid 上界：小于它的写入事务都已结束（只看本库能证明的范围，见 recordChangesWatermark）
	Watermark string `gorm:"column:watermark"`
	// Head 是水位以下最后一行的位置；表空时为已清理下界
	Head RecordChangePosition `gorm:"embedded"`
	// Purged 是已清理的最大位置：早于它的游标可能漏掉被删的行
	Purged RecordChangePosition `gorm:"embedded;embeddedPrefix:purged_"`
	// Database 是库实例标识（system_identifier / timeline / 库 oid），换库或恢复到别的实例后游标失效
	Database string `gorm:"column:database"`
	// PendingSince 是水位及以上最早一行的写入时间（还不能投递的变更）；没有时为 0
	PendingSince int64 `gorm:"column:pending_since"`
}

// recordChangeScanLimit 带集合 / 记录过滤的读取每次最多扫描的变更行：过滤掉的行再多也只扫这么多，返回中间位置，下次接着扫
const recordChangeScanLimit = 5000

// recordChangesWatermark 是本库的投递水位：当前快照里仍在运行的事务中，最小的那个不能证明属于别的库的 xid；没有时取快照 xmax。
// 别的库的事务只有在 pg_stat_activity / pg_prepared_xacts 里看得到（同角色或有 pg_read_all_stats）时才被排除，
// 看不到的一律当作本库事务，此时退化为整个集群的 xmin（投递延迟受集群内最长事务约束）。
// 排除用 NOT EXISTS：NOT IN 的子查询一旦出现 NULL 就会排除全部 xid，水位退化成 xmax，直接跳过正在运行的事务。
// 子查询里的 xip 列必须显式限定（xip.x）：other 也有一列 x，不限定时会解析成内层的 o.x，条件恒真，同样退化成 xmax。
const recordChangesWatermark = `WITH snap AS (SELECT pg_current_snapshot() AS s),
 other AS (SELECT backend_xid::text AS x FROM pg_stat_activity WHERE backend_xid IS NOT NULL AND datname IS DISTINCT FROM current_database()
  UNION ALL SELECT transaction::text FROM pg_prepared_xacts WHERE database <> current_database())
 SELECT COALESCE((SELECT min(xip.x) FROM snap, pg_snapshot_xip(snap.s) AS xip(x)
   WHERE NOT EXISTS (SELECT 1 FROM other o WHERE o.x = ((xip.x::text)::numeric % 4294967296)::text)),
  (SELECT pg_snapshot_xmax(s) FROM snap))::text`

// recordChangesDatabase 是库实例标识；pg_control_* 不可执行时只用库 oid
const recordChangesDatabase = `CASE WHEN has_function_privilege('pg_control_system()', 'EXECUTE') AND has_function_privilege('pg_control_checkpoint()', 'EXECUTE')
 THEN (SELECT system_identifier::text FROM pg_control_system()) || '.' || (SELECT timeline_id::text FROM pg_control_checkpoint()) ELSE '' END
 || '/' || (SELECT oid::text FROM pg_database WHERE datname = current_database())`

// ReadRecordChangesWindow 读取当前快照的水位、表头、已清理下界与库实例标识。
func ReadRecordChangesWindow(ctx context.Context, tx *gorm.DB) (RecordChangesWindow, error) {
	var window RecordChangesWindow
	if err := tx.WithContext(ctx).Raw(recordChangesWatermark).Scan(&window.Watermark).Error; err != nil {
		return window, err
	}
	var rows []RecordChangesWindow
	err := tx.WithContext(ctx).Raw(`SELECT
 COALESCE((SELECT xid::text FROM public.uns_record_change c WHERE c.xid < ?::xid8 ORDER BY c.xid DESC, c.seq DESC LIMIT 1), '0') AS xid_text,
 COALESCE((SELECT seq FROM public.uns_record_change c WHERE c.xid < ?::xid8 ORDER BY c.xid DESC, c.seq DESC LIMIT 1), 0) AS seq,
 COALESCE((SELECT xid::text FROM public.uns_record_change_purged WHERE id = 1), '0') AS purged_xid_text,
 COALESCE((SELECT seq FROM public.uns_record_change_purged WHERE id = 1), 0) AS purged_seq,
 COALESCE((SELECT created_at FROM public.uns_record_change c WHERE c.xid >= ?::xid8 ORDER BY c.xid, c.seq LIMIT 1), 0) AS pending_since,
 `+recordChangesDatabase+` AS database`, window.Watermark, window.Watermark, window.Watermark).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return window, err
	}
	rows[0].Watermark = window.Watermark
	// 已清理下界截到水位以下：本库有超过保留期的长事务时，下界可能越过水位，新订阅的起点不能越过这个事务将来写入的行
	if watermark, _ := strconv.ParseUint(window.Watermark, 10, 64); watermark > 0 {
		below := RecordChangePosition{XID: strconv.FormatUint(watermark-1, 10), Seq: math.MaxInt64}
		if below.Less(rows[0].Purged) {
			rows[0].Purged = below
		}
	}
	rows[0].Head = rows[0].Head.Max(rows[0].Purged)
	return rows[0], nil
}

// ReadRecordChanges 读取位置之后、水位以下的变更；collections 为空表示不按集合过滤。按表里的 xid8 列排序（走 (xid, seq) 索引）。
// 返回的位置：取满 limit 时是最后一行；带过滤时最多扫 recordChangeScanLimit 行，扫满时是扫到的最后一行；
// 否则推进到窗口表头（没有匹配行也前进，下次不再重扫这一段）。more 表示返回位置之后还有已结束事务的行没读。
func ReadRecordChanges(ctx context.Context, tx *gorm.DB, window RecordChangesWindow, after RecordChangePosition, collections, recordIDs []string, limit int) ([]RecordChange, RecordChangePosition, bool, error) {
	q := tx.WithContext(ctx).Table("public.uns_record_change AS c").Select("c.seq, c.xid::text AS xid_text, c.collection_id, c.record_id, c.revision, c.change, c.created_at").
		Where("(c.xid, c.seq) > (?::xid8, ?) AND c.xid < ?::xid8", after.XID, after.Seq, window.Watermark)
	var boundary []RecordChangePosition
	if len(collections) > 0 || len(recordIDs) > 0 {
		// 扫描上限：先按 (xid, seq) 找到第 recordChangeScanLimit 行，过滤只在这一段里做
		if err := tx.WithContext(ctx).Raw(`SELECT c.xid::text AS xid_text, c.seq FROM public.uns_record_change c
 WHERE (c.xid, c.seq) > (?::xid8, ?) AND c.xid < ?::xid8 ORDER BY c.xid, c.seq OFFSET ? LIMIT 1`, after.XID, after.Seq, window.Watermark, recordChangeScanLimit-1).Scan(&boundary).Error; err != nil {
			return nil, after, false, err
		}
		if len(boundary) > 0 {
			q = q.Where("(c.xid, c.seq) <= (?::xid8, ?)", boundary[0].XID, boundary[0].Seq)
		}
	}
	if len(collections) > 0 {
		q = q.Where("c.collection_id IN ?", collections)
	}
	if len(recordIDs) > 0 {
		q = q.Where("c.record_id IN ?", recordIDs)
	}
	var rows []RecordChange
	if err := q.Order("c.xid, c.seq").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, after, false, err
	}
	if len(rows) == limit {
		last := rows[len(rows)-1]
		return rows, RecordChangePosition{XID: last.XID, Seq: last.Seq}, true, nil
	}
	if len(boundary) > 0 {
		return rows, boundary[0], true, nil
	}
	return rows, after.Max(window.Head), false, nil
}

// RecordChangesOldest 返回保留窗口内最早一行的写入时间；没有行时为 0。
func RecordChangesOldest(ctx context.Context, tx *gorm.DB) (int64, error) {
	var oldest *int64
	err := tx.WithContext(ctx).Raw("SELECT min(created_at) FROM public.uns_record_change").Scan(&oldest).Error
	if oldest == nil {
		return 0, err
	}
	return *oldest, err
}

// PurgeRecordChanges 删除早于 before 的变更行，每次最多 limit 行，并把已清理下界推进到删掉的最大位置；返回删除行数。
func PurgeRecordChanges(ctx context.Context, tx *gorm.DB, before int64, limit int) (int64, error) {
	var deleted int64
	err := tx.WithContext(ctx).Raw(`WITH d AS (
 DELETE FROM public.uns_record_change WHERE seq IN (SELECT seq FROM public.uns_record_change WHERE created_at < ? ORDER BY seq LIMIT ?) RETURNING xid, seq),
 m AS (SELECT xid, seq FROM d ORDER BY xid DESC, seq DESC LIMIT 1),
 u AS (INSERT INTO public.uns_record_change_purged (id, xid, seq) SELECT 1, xid, seq FROM m
  ON CONFLICT (id) DO UPDATE SET xid = EXCLUDED.xid, seq = EXCLUDED.seq
  WHERE (public.uns_record_change_purged.xid, public.uns_record_change_purged.seq) < (EXCLUDED.xid, EXCLUDED.seq) RETURNING 1)
 SELECT count(*) FROM d`, before, limit).Scan(&deleted).Error
	return deleted, err
}
