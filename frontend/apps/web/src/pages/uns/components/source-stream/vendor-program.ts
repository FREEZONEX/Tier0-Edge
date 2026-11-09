import type { Source } from '@/apis/core-api/source-stream';
import { internalResourceConfig } from './program-document';

/** Returns a complete registration block, using selected stable IDs, never credentials. */
export function vendorCollectionBody(
  source: Source,
  resourceRef: string,
  topicId: string,
  entryId: string
): string | undefined {
  const resource = source.resources.find((item) => item.id === resourceRef);
  if (
    !source.document.sourceType ||
    resource?.metadata?.readOnly !== true ||
    resource.metadata.sourceType !== source.document.sourceType ||
    !topicId
  )
    return;
  const paging = ['sap-odata', 'salesforce'].includes(source.document.sourceType);
  return `    const source = api.sources.use({ id: ${JSON.stringify(String(source.id))}, resources: [${JSON.stringify(resourceRef)}] });
    const topic = api.uns.use({ ${internalResourceConfig(topicId, 'write')} });
    // Manual read example. Input: { params: {}, query: {}, data: {} }.
    // Source owns authentication, sessions and native signing. No live calls in preview.
    api.stream.onRequest({ id: ${JSON.stringify(entryId)} }, async function (context) {
      const options = Object.assign({}, context.input || {});
${
  paging
    ? `      const cursor = await api.stream.checkpoint.get();
      if (cursor && cursor.next) options.next = cursor.next;
`
    : ''
}      const page = await source.read(${JSON.stringify(resourceRef)}, options);
      if (page.status < 200 || page.status >= 300 || page.error != null) {
        throw new Error('Native resource request failed');
      }
      await topic.write({
        // Use a stable native entity ID when mapping individual business records.
        recordId: ${JSON.stringify(`source-${source.id}-${resourceRef.replace(/[^a-zA-Z0-9_-]/g, '-')}`)},
        record: {
          sourceId: ${JSON.stringify(String(source.id))},
          resourceRef: ${JSON.stringify(resourceRef)},
          data: { native: page.data },
        },
      });
${
  paging
    ? `      // Buffered cursor commits in the same transaction as the UNS output.
      // A final page clears the page cursor. Configure incremental windows separately.
      await api.stream.checkpoint.set(page.pagination && page.pagination.next
        ? { next: page.pagination.next } : null);
`
    : ''
}      return { status: page.status, data: page.data, pagination: page.pagination || null };
    });`;
}
