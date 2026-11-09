# Salesforce Pub/Sub client

`pubsub.proto` is the Salesforce `eventbus.v1` definition supplied by the source portal at `/portal/api/datasources/crm/pubsub.proto` (retrieved 2026-11-05), with trailing spaces/tabs removed from 30 lines. No protocol declarations or other content were changed. Its original `go_package` points to `github.com/developerforce/pub-sub-api/go/proto`.

Original download SHA-256: `0a651295ff3ff13551b3ae7cb6fe87cf91b749a8ae33b6079617dbc62f304eb5`.

Normalized source SHA-256: `719488cd7da414248b303bd655a0a6b9b0bc347c3ac775b50f1558e60895b766`.

The normalized source produces the same descriptor set (without source-location information) and identical generated Go clients as the original download.

Generated with protoc 3.19.4, protoc-gen-go 1.36.11 and protoc-gen-go-grpc 1.6.2. From `backend/`:

```text
protoc -I internal/salesforcepubsub --go_out=internal/salesforcepubsub --go_opt=paths=source_relative --go_opt=Mpubsub.proto=backend/internal/salesforcepubsub --go-grpc_out=internal/salesforcepubsub --go-grpc_opt=paths=source_relative --go-grpc_opt=Mpubsub.proto=backend/internal/salesforcepubsub internal/salesforcepubsub/pubsub.proto
```

The adapter only calls GetTopic, GetSchema and Subscribe. Generated Publish and ManagedSubscribe methods are not exposed as Source resources. Runtime is pure Go (gRPC and goavro); protoc, Python and Java are build-time/development tools, not installation dependencies.
