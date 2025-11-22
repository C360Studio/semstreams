package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/c360/semstreams/errors"
	gtypes "github.com/c360/semstreams/graph"
)

// Mutation subject patterns
const (
	// Entity mutations
	SubjectEntityCreate          = "graph.mutation.entity.create"
	SubjectEntityUpdate          = "graph.mutation.entity.update"
	SubjectEntityDelete          = "graph.mutation.entity.delete"
	SubjectEntityCreateWithEdges = "graph.mutation.entity.create-with-edges"
	SubjectEntityUpdateWithEdges = "graph.mutation.entity.update-with-edges"

	// Edge mutations
	SubjectEdgeAdd    = "graph.mutation.edge.add"
	SubjectEdgeRemove = "graph.mutation.edge.remove"

	// Default timeout for mutation operations
	DefaultMutationTimeout = 5 * time.Second
)

// setupMutationHandlers subscribes to all mutation subjects
func (p *Processor) setupMutationHandlers(ctx context.Context) error {
	// Check for cancellation before setup
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if p.natsClient == nil {
		return errors.WrapFatal(nil, "GraphProcessor", "setupMutationHandlers", "NATS client not initialized")
	}

	// Get raw NATS connection for request/reply pattern
	nc := p.natsClient.GetConnection()
	if nc == nil {
		return errors.WrapFatal(nil, "GraphProcessor", "setupMutationHandlers", "NATS connection not available")
	}

	// Entity mutations
	handlers := map[string]nats.MsgHandler{
		SubjectEntityCreate:          p.handleEntityCreate,
		SubjectEntityUpdate:          p.handleEntityUpdate,
		SubjectEntityDelete:          p.handleEntityDelete,
		SubjectEntityCreateWithEdges: p.handleEntityCreateWithEdges,
		SubjectEntityUpdateWithEdges: p.handleEntityUpdateWithEdges,
		SubjectEdgeAdd:               p.handleEdgeAdd,
		SubjectEdgeRemove:            p.handleEdgeRemove,
	}

	// Subscribe to each mutation subject using raw NATS for request/reply
	for subject, handler := range handlers {
		sub, err := nc.Subscribe(subject, handler)
		if err != nil {
			return errors.Wrap(err, "GraphProcessor", "setupMutationHandlers",
				fmt.Sprintf("failed to subscribe to %s", subject))
		}

		p.logger.Info("Subscribed to mutation subject",
			"subject", subject,
			"queue", sub.Queue,
		)
	}

	p.logger.Info("NATS mutation handlers initialized",
		"handlers", len(handlers),
	)

	return nil
}

// handleEntityCreate handles entity creation requests
func (p *Processor) handleEntityCreate(msg *nats.Msg) {
	// Check if processor is ready to handle requests
	if !p.IsReady() {
		p.respondWithError(msg,
			errors.WrapTransient(nil, "GraphProcessor", "handleEntityCreate", "processor not ready"),
			"", "")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultMutationTimeout)
	defer cancel()

	// Parse request
	var req gtypes.CreateEntityRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		p.respondWithError(msg, err, req.TraceID, req.RequestID)
		return
	}

	// Validate request
	if req.Entity == nil {
		p.respondWithError(msg,
			errors.WrapInvalid(nil, "GraphProcessor", "handleEntityCreate", "entity is required"),
			req.TraceID, req.RequestID)
		return
	}

	// Create entity using DataManager
	entity, err := p.entityManager.CreateEntity(ctx, req.Entity)

	// Build response
	resp := gtypes.CreateEntityResponse{
		MutationResponse: gtypes.NewMutationResponse(err == nil, err, req.TraceID, req.RequestID),
		Entity:           entity,
	}

	// Send response
	p.respond(msg, resp)
}

// handleEntityUpdate handles entity update requests
func (p *Processor) handleEntityUpdate(msg *nats.Msg) {
	// Check if processor is ready to handle requests
	if !p.IsReady() {
		p.respondWithError(msg,
			errors.WrapTransient(nil, "GraphProcessor", "handleEntityUpdate", "processor not ready"),
			"", "")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultMutationTimeout)
	defer cancel()

	// Parse request
	var req gtypes.UpdateEntityRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		p.respondWithError(msg, err, req.TraceID, req.RequestID)
		return
	}

	// Validate request
	if req.Entity == nil {
		p.respondWithError(msg,
			errors.WrapInvalid(nil, "GraphProcessor", "handleEntityUpdate", "entity is required"),
			req.TraceID, req.RequestID)
		return
	}

	// Update entity using DataManager
	entity, err := p.entityManager.UpdateEntity(ctx, req.Entity)

	// Build response
	resp := gtypes.UpdateEntityResponse{
		MutationResponse: gtypes.NewMutationResponse(err == nil, err, req.TraceID, req.RequestID),
		Entity:           entity,
	}
	if entity != nil {
		resp.Version = int64(entity.Version)
	}

	// Send response
	p.respond(msg, resp)
}

// handleEntityDelete handles entity deletion requests
func (p *Processor) handleEntityDelete(msg *nats.Msg) {
	// Check if processor is ready to handle requests
	if !p.IsReady() {
		p.respondWithError(msg,
			errors.WrapTransient(nil, "GraphProcessor", "handleEntityDelete", "processor not ready"),
			"", "")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultMutationTimeout)
	defer cancel()

	// Parse request
	var req gtypes.DeleteEntityRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		p.respondWithError(msg, err, req.TraceID, req.RequestID)
		return
	}

	// Validate request
	if req.EntityID == "" {
		p.respondWithError(msg,
			errors.WrapInvalid(nil, "GraphProcessor", "handleEntityDelete", "entity ID is required"),
			req.TraceID, req.RequestID)
		return
	}

	// Delete entity using DataManager
	err := p.entityManager.DeleteEntity(ctx, req.EntityID)

	// Build response
	resp := gtypes.DeleteEntityResponse{
		MutationResponse: gtypes.NewMutationResponse(err == nil, err, req.TraceID, req.RequestID),
		Deleted:          err == nil,
	}

	// Send response
	p.respond(msg, resp)
}

// handleEntityCreateWithEdges handles atomic entity+edges creation
func (p *Processor) handleEntityCreateWithEdges(msg *nats.Msg) {
	// Check if processor is ready to handle requests
	if !p.IsReady() {
		p.respondWithError(msg,
			errors.WrapTransient(nil, "GraphProcessor", "handleEntityCreateWithEdges", "processor not ready"),
			"", "")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultMutationTimeout)
	defer cancel()

	// Parse request
	var req gtypes.CreateEntityWithEdgesRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		p.respondWithError(msg, err, req.TraceID, req.RequestID)
		return
	}

	// Validate request
	if req.Entity == nil {
		p.respondWithError(msg,
			errors.WrapInvalid(nil, "GraphProcessor", "handleEntityCreateWithEdges", "entity is required"),
			req.TraceID, req.RequestID)
		return
	}

	// Create entity with edges atomically
	entity, err := p.entityManager.CreateEntityWithEdges(ctx, req.Entity, req.Edges)

	// Build response
	resp := gtypes.CreateEntityWithEdgesResponse{
		MutationResponse: gtypes.NewMutationResponse(err == nil, err, req.TraceID, req.RequestID),
		Entity:           entity,
		EdgesAdded:       len(req.Edges),
	}

	// Send response
	p.respond(msg, resp)
}

// handleEntityUpdateWithEdges handles atomic entity+edges update
func (p *Processor) handleEntityUpdateWithEdges(msg *nats.Msg) {
	// Check if processor is ready to handle requests
	if !p.IsReady() {
		p.respondWithError(msg,
			errors.WrapTransient(nil, "GraphProcessor", "handleEntityUpdateWithEdges", "processor not ready"),
			"", "")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultMutationTimeout)
	defer cancel()

	// Parse request
	var req gtypes.UpdateEntityWithEdgesRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		p.respondWithError(msg, err, req.TraceID, req.RequestID)
		return
	}

	// Validate request
	if req.Entity == nil {
		p.respondWithError(msg,
			errors.WrapInvalid(nil, "GraphProcessor", "handleEntityUpdateWithEdges", "entity is required"),
			req.TraceID, req.RequestID)
		return
	}

	// Update entity with edges atomically
	entity, err := p.entityManager.UpdateEntityWithEdges(ctx, req.Entity, req.AddEdges, req.RemoveEdges)

	// Build response
	resp := gtypes.UpdateEntityWithEdgesResponse{
		MutationResponse: gtypes.NewMutationResponse(err == nil, err, req.TraceID, req.RequestID),
		Entity:           entity,
		EdgesAdded:       len(req.AddEdges),
		EdgesRemoved:     len(req.RemoveEdges),
	}
	if entity != nil {
		resp.Version = int64(entity.Version)
	}

	// Send response
	p.respond(msg, resp)
}

// handleEdgeAdd handles edge addition requests
func (p *Processor) handleEdgeAdd(msg *nats.Msg) {
	// Check if processor is ready to handle requests
	if !p.IsReady() {
		p.respondWithError(msg,
			errors.WrapTransient(nil, "GraphProcessor", "handleEdgeAdd", "processor not ready"),
			"", "")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultMutationTimeout)
	defer cancel()

	// Parse request
	var req gtypes.AddEdgeRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		p.respondWithError(msg, err, req.TraceID, req.RequestID)
		return
	}

	// Validate request
	if req.FromEntityID == "" || req.ToEntityID == "" || req.EdgeType == "" {
		p.respondWithError(msg,
			errors.WrapInvalid(nil, "GraphProcessor", "handleEdgeAdd",
				"from_entity_id, to_entity_id, and edge_type are required"),
			req.TraceID, req.RequestID)
		return
	}

	// Create edge
	edge := gtypes.Edge{
		ToEntityID: req.ToEntityID,
		EdgeType:   req.EdgeType,
		Properties: req.Properties,
		Weight:     req.Weight,
		CreatedAt:  time.Now(),
	}
	if edge.Weight == 0 {
		edge.Weight = 1.0
	}

	// Add edge using EdgeManager
	err := p.edgeManager.AddEdge(ctx, req.FromEntityID, edge)

	// Build response
	resp := gtypes.AddEdgeResponse{
		MutationResponse: gtypes.NewMutationResponse(err == nil, err, req.TraceID, req.RequestID),
	}
	if err == nil {
		resp.Edge = &edge
	}

	// Send response
	p.respond(msg, resp)
}

// handleEdgeRemove handles edge removal requests
func (p *Processor) handleEdgeRemove(msg *nats.Msg) {
	// Check if processor is ready to handle requests
	if !p.IsReady() {
		p.respondWithError(msg,
			errors.WrapTransient(nil, "GraphProcessor", "handleEdgeRemove", "processor not ready"),
			"", "")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultMutationTimeout)
	defer cancel()

	// Parse request
	var req gtypes.RemoveEdgeRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		p.respondWithError(msg, err, req.TraceID, req.RequestID)
		return
	}

	// Validate request
	if req.FromEntityID == "" || req.ToEntityID == "" || req.EdgeType == "" {
		p.respondWithError(msg,
			errors.WrapInvalid(nil, "GraphProcessor", "handleEdgeRemove",
				"from_entity_id, to_entity_id, and edge_type are required"),
			req.TraceID, req.RequestID)
		return
	}

	// Remove edge using EdgeManager
	err := p.edgeManager.RemoveEdge(ctx, req.FromEntityID, req.ToEntityID, req.EdgeType)

	// Build response
	resp := gtypes.RemoveEdgeResponse{
		MutationResponse: gtypes.NewMutationResponse(err == nil, err, req.TraceID, req.RequestID),
		Removed:          err == nil,
	}

	// Send response
	p.respond(msg, resp)
}

// Helper methods

// respond sends a JSON response to a NATS request
func (p *Processor) respond(msg *nats.Msg, response interface{}) {
	data, err := json.Marshal(response)
	if err != nil {
		p.logger.Error("Failed to marshal response",
			"error", err,
			"type", fmt.Sprintf("%T", response),
		)
		// Send error response
		errResp := gtypes.MutationResponse{
			Success:   false,
			Error:     fmt.Sprintf("internal error: failed to marshal response: %v", err),
			Timestamp: time.Now().UnixNano(),
		}
		if errData, err := json.Marshal(errResp); err == nil {
			msg.Respond(errData)
		}
		return
	}

	if err := msg.Respond(data); err != nil {
		p.logger.Error("Failed to send response",
			"error", err,
			"subject", msg.Subject,
		)
	}
}

// respondWithError sends an error response
func (p *Processor) respondWithError(msg *nats.Msg, err error, traceID, requestID string) {
	resp := gtypes.NewMutationResponse(false, err, traceID, requestID)
	p.respond(msg, resp)
}
