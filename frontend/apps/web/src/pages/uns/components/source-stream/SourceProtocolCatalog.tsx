import { useState, type FC } from 'react';
import { Empty, Input } from 'antd';
import { Add, Api, Cloud, Cube, Industry, Search, Wifi } from '@carbon/icons-react';
import { useTranslate } from '@/hooks';
import { type ProtocolDefinition, type SourceTypeDefinition } from '@/apis/core-api/source-stream';
import styles from './index.module.scss';

const catalog = [
  { id: 'mqtt', category: 'messaging', icon: Wifi },
  { id: 'http-client', protocolId: 'http-api', role: 'client' as const, category: 'messaging', icon: Api },
  { id: 'http-server', protocolId: 'http-api', role: 'server' as const, category: 'messaging', icon: Api },
  { id: 'opcua', category: 'industrial', icon: Industry },
  { id: 'modbus-tcp', category: 'industrial', icon: Cube },
  { id: 'usb-hid', category: 'industrial', icon: Cube },
  { id: 'oauth2', category: 'business', icon: Cloud },
  { id: 'edge-sql', category: 'business', icon: Cube },
  { id: 'file-read', category: 'business', icon: Cube },
  { id: 'open-protocol', category: 'industrial', icon: Industry },
];

const SourceProtocolCatalog: FC<{
  protocols: ProtocolDefinition[];
  sourceTypes?: SourceTypeDefinition[];
  onSelect: (protocol: ProtocolDefinition, role?: 'client' | 'server', profile?: SourceTypeDefinition) => void;
}> = ({ protocols, sourceTypes = [], onSelect }) => {
  const t = useTranslate();
  const [search, setSearch] = useState('');
  const query = search.trim().toLocaleLowerCase();
  const entries = catalog.map((item) => ({
    ...item,
    protocol: protocols.find(
      (protocol) => protocol.id === (item.protocolId || item.id) && (!item.role || protocol.roles?.includes(item.role))
    ),
  }));
  const vendors = sourceTypes
    .filter((profile) => profile.id !== 'plant0-simctl')
    .map((profile) => ({
      id: profile.id,
      category: 'business',
      icon: Cloud,
      role: 'client' as const,
      profile,
      protocol: protocols.find((protocol) => protocol.id === profile.protocol),
    }));
  const filtered = [...entries, ...vendors].filter(
    (item) =>
      item.protocol &&
      [
        item.id,
        t(`SourceStream.${'profile' in item ? 'vendorName' : 'catalogName'}.${item.id}`),
        t(`SourceStream.${'profile' in item ? 'vendorHint' : 'catalogHint'}.${item.id}`),
        t(`SourceStream.category.${item.category}`),
      ]
        .join(' ')
        .toLocaleLowerCase()
        .includes(query)
  );
  return (
    <div className={styles['source-catalog']}>
      <Input
        autoFocus
        allowClear
        prefix={<Search size={20} />}
        className={styles['catalog-search']}
        placeholder={t('SourceStream.catalogSearch')}
        aria-label={t('SourceStream.catalogSearch')}
        value={search}
        onChange={(event) => setSearch(event.target.value)}
      />
      <div className={styles['catalog-results']}>
        {filtered.length === 0 && (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('SourceStream.catalogEmpty')} />
        )}
        <div className={styles['protocol-grid']}>
          {filtered.map((entry) => {
            const { id, role, icon: Icon, protocol } = entry;
            const profile = 'profile' in entry ? entry.profile : undefined;
            return (
              <button
                type="button"
                className={styles['protocol-card']}
                key={id}
                onClick={() => protocol && onSelect(protocol, role, profile)}
              >
                <span className={styles['protocol-icon']}>
                  <Icon size={22} />
                </span>
                <span className={styles['protocol-copy']}>
                  <strong>{t(`SourceStream.${profile ? 'vendorName' : 'catalogName'}.${id}`)}</strong>
                </span>
                {protocol && <Add size={20} className={styles['protocol-add']} />}
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
};

export default SourceProtocolCatalog;
