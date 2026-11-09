const END_OF_CENTRAL_DIRECTORY_SIGNATURE = 0x06054b50;
const ZIP64_END_OF_CENTRAL_DIRECTORY_SIGNATURE = 0x06064b50;
const ZIP64_END_OF_CENTRAL_DIRECTORY_LOCATOR_SIGNATURE = 0x07064b50;
const ZIP64_EXTRA_FIELD_SIGNATURE = 0x0001;
const CENTRAL_DIRECTORY_ENTRY_SIGNATURE = 0x02014b50;
const LOCAL_FILE_HEADER_SIGNATURE = 0x04034b50;
const UINT32_PLACEHOLDER = 0xffffffff;
const UINT16_PLACEHOLDER = 0xffff;
const MAX_END_RECORD_SIZE = 65_557;
const MAX_META_SIZE = 1024 * 1024;
const MAX_IMAGE_SIZE = 20 * 1024 * 1024;
const IMAGE_CONTENT_TYPES: Record<string, string> = {
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  png: 'image/png',
  svg: 'image/svg+xml',
  webp: 'image/webp',
};
const IMAGE_EXTENSIONS = ['png', 'jpg', 'jpeg', 'svg', 'webp'] as const;

export interface AppBundleMeta {
  workspaceId: string;
  projectId: string;
  appId: string;
  name?: string;
  description?: string;
  schemaAlias?: string;
  version?: number;
  changelog?: string;
}

export interface AppBundleContents {
  meta: AppBundleMeta;
  icon?: File;
  cover?: File;
}

interface ZipEntry {
  fileName: string;
  compressionMethod: number;
  compressedSize: number;
  uncompressedSize: number;
  localHeaderOffset: number;
}

const getUint64 = (view: DataView, offset: number): number => Number(view.getBigUint64(offset, true));

const findEndOfCentralDirectory = (bytes: Uint8Array) => {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  for (let offset = bytes.byteLength - 22; offset >= 0; offset -= 1) {
    if (view.getUint32(offset, true) === END_OF_CENTRAL_DIRECTORY_SIGNATURE) {
      return offset;
    }
  }
  return -1;
};

// readCentralDirectoryInfo 解析 EOCD 中的 central directory 元信息。
// CD size/offset/total entries 出现 32-bit 占位值时，通过 EOCD 前 20 字节的
// ZIP64 locator（0x07064b50）定位 ZIP64 EOCD（0x06064b50）读取 64-bit 值；
// locator/ZIP64 EOCD 缺失或签名不符均抛 readable error。
const readCentralDirectoryInfo = async (
  file: File,
  tailOffset: number,
  tail: Uint8Array,
  endRecordOffset: number
): Promise<{ totalEntries: number; centralDirectorySize: number; centralDirectoryOffset: number }> => {
  const endRecord = new DataView(tail.buffer, tail.byteOffset + endRecordOffset, tail.byteLength - endRecordOffset);
  const totalEntries32 = endRecord.getUint16(10, true);
  const centralDirectorySize32 = endRecord.getUint32(12, true);
  const centralDirectoryOffset32 = endRecord.getUint32(16, true);

  if (
    totalEntries32 === UINT16_PLACEHOLDER ||
    centralDirectorySize32 === UINT32_PLACEHOLDER ||
    centralDirectoryOffset32 === UINT32_PLACEHOLDER
  ) {
    const locatorOffset = tailOffset + endRecordOffset - 20;
    if (locatorOffset < 0) {
      throw new Error('invalid-zip64');
    }
    const locatorBytes = new Uint8Array(await file.slice(locatorOffset, locatorOffset + 20).arrayBuffer());
    if (locatorBytes.byteLength < 20) {
      throw new Error('invalid-zip64');
    }
    const locator = new DataView(locatorBytes.buffer, locatorBytes.byteOffset, locatorBytes.byteLength);
    if (locator.getUint32(0, true) !== ZIP64_END_OF_CENTRAL_DIRECTORY_LOCATOR_SIGNATURE) {
      throw new Error('invalid-zip64');
    }
    const zip64EocdOffset = getUint64(locator, 8);
    const zip64EocdBytes = new Uint8Array(await file.slice(zip64EocdOffset, zip64EocdOffset + 56).arrayBuffer());
    if (zip64EocdBytes.byteLength < 56) {
      throw new Error('invalid-zip64');
    }
    const zip64Eocd = new DataView(zip64EocdBytes.buffer, zip64EocdBytes.byteOffset, zip64EocdBytes.byteLength);
    if (zip64Eocd.getUint32(0, true) !== ZIP64_END_OF_CENTRAL_DIRECTORY_SIGNATURE) {
      throw new Error('invalid-zip64');
    }
    return {
      totalEntries: getUint64(zip64Eocd, 32),
      centralDirectorySize: getUint64(zip64Eocd, 40),
      centralDirectoryOffset: getUint64(zip64Eocd, 48),
    };
  }

  return {
    totalEntries: totalEntries32,
    centralDirectorySize: centralDirectorySize32,
    centralDirectoryOffset: centralDirectoryOffset32,
  };
};

const findPackageEntries = (bytes: Uint8Array) => {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const decoder = new TextDecoder();
  const entries: ZipEntry[] = [];
  let offset = 0;

  while (offset + 46 <= bytes.byteLength) {
    if (view.getUint32(offset, true) !== CENTRAL_DIRECTORY_ENTRY_SIGNATURE) {
      break;
    }

    const flags = view.getUint16(offset + 8, true);
    const compressionMethod = view.getUint16(offset + 10, true);
    const compressedSize32 = view.getUint32(offset + 20, true);
    const uncompressedSize32 = view.getUint32(offset + 24, true);
    const fileNameLength = view.getUint16(offset + 28, true);
    const extraFieldLength = view.getUint16(offset + 30, true);
    const commentLength = view.getUint16(offset + 32, true);
    const localHeaderOffset32 = view.getUint32(offset + 42, true);
    const nextOffset = offset + 46 + fileNameLength + extraFieldLength + commentLength;

    if (nextOffset > bytes.byteLength) {
      break;
    }

    let compressedSize = compressedSize32;
    let uncompressedSize = uncompressedSize32;
    let localHeaderOffset = localHeaderOffset32;
    // ZIP64 extra field（header id 0x0001）：32-bit 字段为占位值时，
    // 按 uncompressed size → compressed size → local header offset 顺序读 64-bit 值
    if (extraFieldLength > 0) {
      const extraStart = offset + 46 + fileNameLength;
      const extraEnd = extraStart + extraFieldLength;
      let cursor = extraStart;
      while (cursor + 4 <= extraEnd) {
        const headerId = view.getUint16(cursor, true);
        const dataSize = view.getUint16(cursor + 2, true);
        const dataStart = cursor + 4;
        if (dataStart + dataSize > extraEnd) {
          throw new Error('invalid-zip64');
        }
        if (headerId === ZIP64_EXTRA_FIELD_SIGNATURE) {
          let dataCursor = dataStart;
          if (uncompressedSize32 === UINT32_PLACEHOLDER) {
            if (dataCursor + 8 > extraEnd) {
              throw new Error('invalid-zip64');
            }
            uncompressedSize = getUint64(view, dataCursor);
            dataCursor += 8;
          }
          if (compressedSize32 === UINT32_PLACEHOLDER) {
            if (dataCursor + 8 > extraEnd) {
              throw new Error('invalid-zip64');
            }
            compressedSize = getUint64(view, dataCursor);
            dataCursor += 8;
          }
          if (localHeaderOffset32 === UINT32_PLACEHOLDER) {
            if (dataCursor + 8 > extraEnd) {
              throw new Error('invalid-zip64');
            }
            localHeaderOffset = getUint64(view, dataCursor);
            dataCursor += 8;
          }
        }
        cursor = dataStart + dataSize;
      }
    }

    const fileName = decoder.decode(bytes.subarray(offset + 46, offset + 46 + fileNameLength)).replace(/^\.\//, '');
    if (!fileName.endsWith('/')) {
      entries.push({
        fileName,
        compressionMethod,
        compressedSize,
        uncompressedSize,
        localHeaderOffset,
      });
      if ((flags & 0x1) !== 0) {
        throw new Error('encrypted');
      }
    }

    offset = nextOffset;
  }

  return entries;
};

const inflateRaw = async (bytes: Uint8Array) => {
  const input = new Uint8Array(bytes.byteLength);
  input.set(bytes);
  const stream = new Blob([input.buffer]).stream().pipeThrough(new DecompressionStream('deflate-raw'));
  return new Uint8Array(await new Response(stream).arrayBuffer());
};

const readEntryBytes = async (file: File, entry: ZipEntry, maxSize: number) => {
  if (entry.uncompressedSize > maxSize) {
    throw new Error('entry-too-large');
  }

  const localHeaderBytes = new Uint8Array(
    await file.slice(entry.localHeaderOffset, entry.localHeaderOffset + 30).arrayBuffer()
  );
  if (localHeaderBytes.byteLength < 30) {
    throw new Error('invalid-local-header');
  }

  const localHeader = new DataView(localHeaderBytes.buffer, localHeaderBytes.byteOffset, localHeaderBytes.byteLength);
  if (localHeader.getUint32(0, true) !== LOCAL_FILE_HEADER_SIGNATURE) {
    throw new Error('invalid-local-header');
  }

  const fileNameLength = localHeader.getUint16(26, true);
  const extraFieldLength = localHeader.getUint16(28, true);
  const dataOffset = entry.localHeaderOffset + 30 + fileNameLength + extraFieldLength;
  const compressedBytes = new Uint8Array(await file.slice(dataOffset, dataOffset + entry.compressedSize).arrayBuffer());

  let bytes: Uint8Array;
  if (entry.compressionMethod === 0) {
    bytes = compressedBytes;
  } else if (entry.compressionMethod === 8) {
    bytes = await inflateRaw(compressedBytes);
  } else {
    throw new Error('unsupported-compression');
  }
  if (bytes.byteLength !== entry.uncompressedSize) {
    throw new Error('invalid-entry-size');
  }
  return bytes;
};

const entryToFile = async (zipFile: File, entry: ZipEntry) => {
  const extension = entry.fileName.split('.').pop()?.toLowerCase() || '';
  const contentType = IMAGE_CONTENT_TYPES[extension];
  if (!contentType) {
    return undefined;
  }
  const bytes = await readEntryBytes(zipFile, entry, MAX_IMAGE_SIZE);
  const content = new Uint8Array(bytes.byteLength);
  content.set(bytes);
  return new File([content.buffer], entry.fileName.split('/').pop() || entry.fileName, { type: contentType });
};

const packageEntryDirectory = (fileName: string) => {
  const slashIndex = fileName.lastIndexOf('/');
  return slashIndex < 0 ? '' : fileName.slice(0, slashIndex);
};

const findPackageMetaEntry = (entries: ZipEntry[]) => {
  let selected: ZipEntry | undefined;
  let selectedDepth = Number.POSITIVE_INFINITY;
  for (const entry of entries) {
    const segments = entry.fileName.split('/');
    if (segments.at(-1)?.toLowerCase() !== 'meta.json') continue;
    const depth = segments.length - 1;
    if (depth < selectedDepth) {
      selected = entry;
      selectedDepth = depth;
    }
  }
  return selected;
};

const findImageEntry = (entries: ZipEntry[], packageRoot: string, kind: 'icon' | 'cover') => {
  const publicPrefixes =
    kind === 'icon'
      ? ['app-icon', 'app_icon', 'icon']
      : ['app-cover', 'app_cover', 'app-cover-image', 'app_cover_image', 'cover'];
  const rootPrefixes =
    kind === 'icon'
      ? ['icon', 'app-icon', 'app_icon']
      : ['cover', 'app-cover', 'app_cover', 'app-cover-image', 'app_cover_image'];
  const entriesByName = new Map(entries.map((entry) => [entry.fileName.toLowerCase(), entry]));
  const rootPrefix = packageRoot ? `${packageRoot.toLowerCase()}/` : '';

  for (const prefix of publicPrefixes) {
    for (const extension of IMAGE_EXTENSIONS) {
      const entry = entriesByName.get(`${rootPrefix}public/${prefix}.${extension}`);
      if (entry && entry.uncompressedSize > 0) return entry;
    }
  }
  for (const prefix of rootPrefixes) {
    for (const extension of IMAGE_EXTENSIONS) {
      const entry = entriesByName.get(`${rootPrefix}${prefix}.${extension}`);
      if (entry && entry.uncompressedSize > 0) return entry;
    }
  }
  return undefined;
};

const normalizeMeta = (value: unknown): AppBundleMeta => {
  if (!value || typeof value !== 'object') {
    throw new Error('invalid-meta');
  }
  let raw = value as Record<string, unknown>;
  // 兼容 wrapped {"app":{...}} 结构：app 为对象时以其为 meta 本体；
  // app 存在但非对象（字符串/数字/数组等）视为 malformed wrapped，直接拒绝
  if (raw.app !== undefined && raw.app !== null) {
    if (typeof raw.app !== 'object' || Array.isArray(raw.app)) {
      throw new Error('invalid-meta');
    }
    raw = raw.app as Record<string, unknown>;
  }
  const workspaceId = String(raw.workspaceId ?? '').trim();
  const projectId = String(raw.projectId ?? '').trim();
  const appId = String(raw.appId ?? '').trim();
  // 身份契约（与后端 validateReplaceIdentity/EIMP-048 对齐）：Replace 只要求 appId 非空，
  // 只有 appId、缺来源字段（workspaceId/projectId）的包必须能覆盖；workspaceId/projectId
  // 允许为空，仅记录来源上下文。
  if (!appId) {
    throw new Error('invalid-meta');
  }

  return {
    workspaceId,
    projectId,
    appId,
    name: typeof raw.name === 'string' ? raw.name.trim() : undefined,
    description: typeof raw.description === 'string' ? raw.description.trim() : undefined,
    schemaAlias: typeof raw.schemaAlias === 'string' ? raw.schemaAlias.trim() : undefined,
    version: typeof raw.version === 'number' && Number.isInteger(raw.version) ? raw.version : undefined,
    changelog: typeof raw.changelog === 'string' ? raw.changelog.trim() : undefined,
  };
};

export const readAppBundleContents = async (file: File): Promise<AppBundleContents> => {
  const tailOffset = Math.max(0, file.size - MAX_END_RECORD_SIZE);
  const tail = new Uint8Array(await file.slice(tailOffset).arrayBuffer());
  const endRecordOffset = findEndOfCentralDirectory(tail);
  if (endRecordOffset < 0) {
    throw new Error('invalid-zip');
  }

  const { centralDirectorySize, centralDirectoryOffset } = await readCentralDirectoryInfo(
    file,
    tailOffset,
    tail,
    endRecordOffset
  );
  if (centralDirectoryOffset + centralDirectorySize > file.size) {
    throw new Error('invalid-zip');
  }

  const centralDirectory = new Uint8Array(
    await file.slice(centralDirectoryOffset, centralDirectoryOffset + centralDirectorySize).arrayBuffer()
  );
  const entries = findPackageEntries(centralDirectory);
  const metaEntry = findPackageMetaEntry(entries);
  if (!metaEntry) {
    throw new Error('meta-missing');
  }
  const packageRoot = packageEntryDirectory(metaEntry.fileName);

  const metaBytes = await readEntryBytes(file, metaEntry, MAX_META_SIZE);
  if (metaBytes.byteLength > MAX_META_SIZE) {
    throw new Error('meta-too-large');
  }

  try {
    const meta = normalizeMeta(JSON.parse(new TextDecoder().decode(metaBytes)));
    const iconEntry = findImageEntry(entries, packageRoot, 'icon');
    const coverEntry = findImageEntry(entries, packageRoot, 'cover');
    const [icon, cover] = await Promise.all([
      iconEntry ? entryToFile(file, iconEntry) : Promise.resolve(undefined),
      coverEntry ? entryToFile(file, coverEntry) : Promise.resolve(undefined),
    ]);
    return { meta, icon, cover };
  } catch (error) {
    if (error instanceof Error && error.message === 'invalid-meta') {
      throw error;
    }
    throw new Error('invalid-meta');
  }
};

export const readAppBundleMeta = async (file: File): Promise<AppBundleMeta> => {
  const { meta } = await readAppBundleContents(file);
  return meta;
};
