# Native Node.js App examples

Build three tiny, independent ZIPs from the repository root:

```powershell
./scripts/deploy/package-native-app-examples.ps1 -OutputDirectory <new-output-directory>
```

- `hello.zip`: no grants required. Prefixed HTML, runtime API and SSE clock.
- `metadata.zip`: grant `record:read` on selected Collections. Reads App context and active authorized Collection metadata using OpenAPI v2.
- `records.zip`: grant `record:read`, `record:write`, `record:delete`. Creates, reads, updates and soft-deletes records through OpenAPI v2. Update/delete require the current record revision. Use a test Collection with string `title`/`status` fields, or edit the example form JSON to match its schema.

Upload each ZIP on **Connect → Edge → Upload and install**. The metadata/records archives include the shared `uns-server.mjs` as `server.mjs` and `uns-index.html` as `index.html`; the packager supplies these root files automatically. All three examples have no npm dependencies. Their Node.js processes share the platform's public port via `/apps/<appId>/`.

The App-scoped API key stays in the server environment and is never sent to the browser. App code can read only explicitly granted Collections. The example bridge accepts fixed operations rather than arbitrary upstream URLs. These examples execute trusted local code; they are not a hostile-code sandbox.

Validated on the real Windows native runtime using a separate PostgreSQL database: browser upload, install progress, live status, stop/start, prefixed HTML/API/SSE, metadata and business CRUD. A failing replacement retained the working previous version. MCP and final installer tests remain separate delivery checks.
