import { Modal } from 'antd';
import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router';
import { coreApi } from '@/apis/core-api/core-adapter';
import { Close } from '@/components/lucide-icon/carbon';
import { notifyNotificationChanged } from '@/components/notification-center/notification-events';
import { useMediaSize } from '@/hooks';

// 全局待展示提醒层（TASK-030）：页面顶部横幅条 + 强提醒 Modal。
// 数据源：GET /api/core/notify/prompt/list（有效横幅 + 未处理强提醒）；
// 任意交互（查看详情 / 关闭 / ×）→ 同时置已处理与已读：
//   handle（is_handled=1，不再弹出）+ read（is_read=1，未读数与列表同步）。
// 只置已处理会让用户看完强提醒后，铃铛上仍挂着这条未读。

interface BannerItem {
  linkId: number;
  title: string;
  content: string;
  link?: string;
}

interface ModalItem {
  linkId: number;
  title: string;
  content: string;
  link?: string;
}

interface PromptResp {
  banners: BannerItem[];
  modals: ModalItem[];
}

const POLL_INTERVAL_MS = 60_000;

const GlobalNoticeAlerts = () => {
  const [banners, setBanners] = useState<BannerItem[]>([]);
  const [modalItem, setModalItem] = useState<ModalItem | null>(null);
  const navigate = useNavigate();
  const { isH5 } = useMediaSize();

  const load = useCallback(async () => {
    try {
      const res = await coreApi.get('/notify/prompt/list', { _noMessage: true });
      const data = (res || { banners: [], modals: [] }) as PromptResp;
      setBanners(data.banners || []);
      setModalItem(
        (current) => (data.modals || []).find((m) => m.linkId === current?.linkId) || (data.modals || [])[0] || null
      );
    } catch {
      // 静默：提醒层失败不影响主页面
    }
  }, []);

  useEffect(() => {
    load();
    const timer = setInterval(load, POLL_INTERVAL_MS);
    const refresh = () => window.setTimeout(load, 0);
    window.addEventListener('tier0:notifications-changed', refresh);
    return () => {
      clearInterval(timer);
      window.removeEventListener('tier0:notifications-changed', refresh);
    };
  }, [load]);

  // 用户已经看到并操作了这条强提醒：既不再弹，也不该继续算未读。
  // 两个请求互不依赖，一个失败不影响另一个（下次加载会再提示）。
  const handle = async (linkId: number) => {
    const results = await Promise.allSettled([
      coreApi.post('/notify/user-message/handle', { id: linkId }),
      coreApi.post('/notify/user-message/read', { ids: [linkId] }),
    ]);
    if (results.some((result) => result.status === 'fulfilled')) {
      // 让铃铛未读数与通知中心列表立刻跟上
      notifyNotificationChanged();
    }
  };

  const dismissBanner = async (item: BannerItem) => {
    setBanners((current) => current.filter((b) => b.linkId !== item.linkId));
    await handle(item.linkId);
  };

  const dismissModal = async () => {
    const item = modalItem;
    setModalItem(null);
    if (item) await handle(item.linkId);
  };

  return (
    <>
      {banners.length > 0 && (
        <div
          data-testid="global-banners"
          style={{
            position: 'relative',
            zIndex: 1000,
            background: 'var(--ui-warning-bg, #fffbe6)',
            borderBottom: '1px solid var(--ui-border-color, #ffe58f)',
            padding: '6px 16px',
            display: 'flex',
            flexDirection: 'column',
            gap: 4,
          }}
        >
          {banners.map((b) => (
            <div key={b.linkId} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span style={{ color: 'var(--ui-warning-color, #d48806)', fontWeight: 600 }}>📢</span>
              <strong style={{ marginRight: 4 }}>{b.title}</strong>
              <span style={{ flex: 1, color: 'var(--ui-text-color)' }}>{b.content}</span>
              {b.link && (
                <a
                  style={{ whiteSpace: 'nowrap' }}
                  onClick={() => {
                    if (b.link?.startsWith('/')) navigate(b.link);
                    else window.open(b.link, '_blank');
                  }}
                >
                  查看详情 →
                </a>
              )}
              <Close
                size={16}
                style={{ cursor: 'pointer', color: 'var(--ui-text-color-secondary, #999)' }}
                onClick={() => void dismissBanner(b)}
              />
            </div>
          ))}
        </div>
      )}

      <Modal
        open={!!modalItem}
        onOk={async () => {
          const link = modalItem?.link;
          await dismissModal();
          if (link) {
            if (link.startsWith('/')) navigate(link);
            else window.open(link, '_blank');
          }
        }}
        onCancel={dismissModal}
        okText={modalItem?.link ? '查看详情' : '知道了'}
        cancelText="关闭"
        width={isH5 ? '92%' : 480}
        zIndex={2000}
        destroyOnHidden
        title={modalItem?.title}
      >
        <div style={{ whiteSpace: 'pre-wrap' }}>{modalItem?.content}</div>
      </Modal>
    </>
  );
};

export default GlobalNoticeAlerts;
