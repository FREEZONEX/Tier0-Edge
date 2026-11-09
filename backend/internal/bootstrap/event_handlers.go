package bootstrap

import (
	"context"
	"strconv"

	"backend/internal/domain/dataingest"
	"backend/internal/events"
	"backend/internal/infra/outbox"
	"backend/internal/repo"

	"github.com/zeromicro/go-zero/core/logx"
)

func registerEventHandlers(app *App) {
	if app == nil {
		return
	}
	bus := events.Default()
	outboxSvc := outbox.New()
	bus.OnInTx(events.UnsNodeSaving{}, 10, "uns.schema.validate", func(ctx context.Context, event any) error {
		e, ok := event.(events.UnsNodeSaving)
		if !ok || e.Node.Type != 2 {
			return nil
		}
		return dataingest.ValidateSchema(e.Node.Schema)
	})
	bus.OnAfterCommit(events.UnsNodeCreated{}, 10, "dataingest.definition.invalidate.created", func(ctx context.Context, event any) error {
		node := event.(events.UnsNodeCreated).Node
		if resolver := app.DataIngest.Resolver(); resolver != nil {
			resolver.InvalidateNode(node)
		}
		clearLatestCache(ctx, app, []repo.UnsNode{node})
		app.DataQuality.ApplyNodePolicies([]repo.UnsNode{node})
		return nil
	})
	bus.OnAfterCommit(events.UnsNodesCreated{}, 10, "dataingest.definition.invalidate.createdBatch", func(ctx context.Context, event any) error {
		nodes := event.(events.UnsNodesCreated).Nodes
		if resolver := app.DataIngest.Resolver(); resolver != nil {
			resolver.InvalidateNodes(nodes)
		}
		clearLatestCache(ctx, app, nodes)
		app.DataQuality.ApplyNodePolicies(nodes)
		return nil
	})
	bus.OnAfterCommit(events.UnsNodesUpdated{}, 10, "dataingest.definition.invalidate.updatedBatch", func(ctx context.Context, event any) error {
		nodes := event.(events.UnsNodesUpdated).Nodes
		if resolver := app.DataIngest.Resolver(); resolver != nil {
			resolver.InvalidateNodes(nodes)
		}
		clearLatestCache(ctx, app, nodes)
		app.DataQuality.ApplyNodePolicies(nodes)
		return nil
	})
	bus.OnAfterCommit(events.UnsNodeUpdated{}, 10, "dataingest.definition.invalidate.updated", func(ctx context.Context, event any) error {
		node := event.(events.UnsNodeUpdated).Node
		if resolver := app.DataIngest.Resolver(); resolver != nil {
			resolver.InvalidateNode(node)
		}
		clearLatestCache(ctx, app, []repo.UnsNode{node})
		app.DataQuality.ApplyNodePolicies([]repo.UnsNode{node})
		return nil
	})
	bus.OnAfterCommit(events.UnsNodeDeleted{}, 10, "dataingest.definition.invalidate.deleted", func(ctx context.Context, event any) error {
		nodes := event.(events.UnsNodeDeleted).Nodes
		if resolver := app.DataIngest.Resolver(); resolver != nil {
			resolver.InvalidateNodes(nodes)
		}
		clearLatestCache(ctx, app, nodes)
		app.DataQuality.RemoveNodePolicies(ctx, nodes)
		return nil
	})
	bus.OnAfterCommit(events.UnsNodeRestored{}, 10, "dataingest.definition.invalidate.restored", func(ctx context.Context, event any) error {
		nodes := event.(events.UnsNodeRestored).Nodes
		if resolver := app.DataIngest.Resolver(); resolver != nil {
			resolver.InvalidateNodes(nodes)
		}
		clearLatestCache(ctx, app, nodes)
		app.DataQuality.ApplyNodePolicies(nodes)
		return nil
	})
	bus.OnAfterCommit(events.UnsNodesSyncHidden{}, 10, "dataingest.definition.invalidate.syncHidden", func(ctx context.Context, event any) error {
		nodes := event.(events.UnsNodesSyncHidden).Nodes
		if resolver := app.DataIngest.Resolver(); resolver != nil {
			resolver.InvalidateNodes(nodes)
		}
		clearLatestCache(ctx, app, nodes)
		app.DataQuality.RemoveNodePolicies(ctx, nodes)
		return nil
	})
	bus.OnAfterCommit(events.UnsNodesSyncRestored{}, 10, "dataingest.definition.invalidate.syncRestored", func(ctx context.Context, event any) error {
		nodes := event.(events.UnsNodesSyncRestored).Nodes
		if resolver := app.DataIngest.Resolver(); resolver != nil {
			resolver.InvalidateNodes(nodes)
		}
		clearLatestCache(ctx, app, nodes)
		app.DataQuality.ApplyNodePolicies(nodes)
		return nil
	})
	bus.OnAfterCommit(events.UnsNodeForceDeleted{}, 10, "dataingest.definition.invalidate.forceDeleted", func(ctx context.Context, event any) error {
		nodes := event.(events.UnsNodeForceDeleted).Nodes
		if resolver := app.DataIngest.Resolver(); resolver != nil {
			resolver.InvalidateNodes(nodes)
		}
		clearLatestCache(ctx, app, nodes)
		app.DataQuality.RemoveNodePolicies(ctx, nodes)
		return nil
	})

	bus.OnAsync(events.UnsNodeCreated{}, 20, "uns.physical.ensure.created", func(ctx context.Context, event any) error {
		e := event.(events.UnsNodeCreated)
		return enqueueUnsPhysicalEnsure(ctx, outboxSvc, []repo.UnsNode{e.Node})
	})
	bus.OnAsync(events.UnsNodesCreated{}, 20, "uns.physical.ensure.createdBatch", func(ctx context.Context, event any) error {
		return enqueueUnsPhysicalEnsure(ctx, outboxSvc, event.(events.UnsNodesCreated).Nodes)
	})
	bus.OnAsync(events.UnsNodeUpdated{}, 20, "uns.physical.ensure.updated", func(ctx context.Context, event any) error {
		e := event.(events.UnsNodeUpdated)
		return enqueueUnsPhysicalEnsure(ctx, outboxSvc, []repo.UnsNode{e.Node})
	})
	bus.OnAsync(events.UnsNodeDeleted{}, 20, "uns.deleted.cleanup", func(ctx context.Context, event any) error {
		e := event.(events.UnsNodeDeleted)
		return outboxSvc.Enqueue(ctx, "uns.deleted.cleanup", "uns", strconv.FormatInt(e.RootID, 10), e)
	})
	bus.OnAsync(events.UnsNodeRestored{}, 20, "uns.restored.sync", func(ctx context.Context, event any) error {
		e := event.(events.UnsNodeRestored)
		return outboxSvc.Enqueue(ctx, "uns.restored.sync", "uns", strconv.FormatInt(e.RootID, 10), e)
	})
	bus.OnAsync(events.UnsNodeForceDeleted{}, 20, "uns.physical.delete", func(ctx context.Context, event any) error {
		e := event.(events.UnsNodeForceDeleted)
		payload := map[string]any{"nodes": e.Nodes, "deleteFlow": e.DeleteFlow}
		return outboxSvc.Enqueue(ctx, "uns.physical.delete", "uns", strconv.FormatInt(e.RootID, 10), payload)
	})
}

func enqueueUnsPhysicalEnsure(ctx context.Context, outboxSvc *outbox.Service, nodes []repo.UnsNode) error {
	physical := make([]repo.UnsNode, 0, len(nodes))
	for _, node := range nodes {
		if node.Type == 2 && node.EnableHistory == 1 {
			physical = append(physical, node)
		}
	}
	if len(physical) == 0 {
		return nil
	}
	aggregateID := strconv.FormatInt(physical[0].ID, 10)
	return outboxSvc.Enqueue(ctx, unsPhysicalEnsureEventType, "uns", aggregateID, unsPhysicalEnsureRequest{Nodes: physical})
}

func clearLatestCache(ctx context.Context, app *App, nodes []repo.UnsNode) {
	if app == nil || app.DataIngest == nil || len(nodes) == 0 {
		return
	}
	if err := app.DataIngest.ClearLatest(ctx, nodes); err != nil {
		logx.WithContext(ctx).Errorf("dataingest latest cache clear failed err=%v", err)
	}
}
