package mock_engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
)

// MockEngine provides a mock implementation of an Ethereum engine for testing.
type MockEngine struct {
	server   *http.Server
	state    *EngineState
	behavior EngineBehavior
}

// NewMockEngine creates a new mock engine listening on the given address.
func NewMockEngine(addr string, behavior EngineBehavior) *MockEngine {
	m := &MockEngine{
		state: &EngineState{
			Payloads: make(map[engine.PayloadID]*engine.ExecutionPayloadEnvelope),
			Requests: make([]RecordedRequest, 0),
		},
		behavior: behavior,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Jsonrpc string            `json:"jsonrpc"`
			Method  string            `json:"method"`
			Params  []json.RawMessage `json:"params"`
			ID      interface{}       `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		m.state.Mu.Lock()
		m.state.Requests = append(m.state.Requests, RecordedRequest{
			Method: req.Method,
			Params: req.Params,
		})
		m.state.Mu.Unlock()

		var respData interface{}
		var respErr *JsonRPCError

		switch req.Method {
		case "engine_newPayloadV4":
			if len(req.Params) < 3 {
				respErr = &JsonRPCError{Code: InvalidParams, Message: "invalid params"}
				break
			}
			var payload engine.ExecutableData
			if err := json.Unmarshal(req.Params[0], &payload); err != nil {
				respErr = &JsonRPCError{Code: InvalidParams, Message: "invalid payload"}
				break
			}
			var versionedHashes []common.Hash
			if err := json.Unmarshal(req.Params[1], &versionedHashes); err != nil {
				respErr = &JsonRPCError{Code: InvalidParams, Message: "invalid versioned hashes"}
				break
			}
			var parentBeaconBlockRoot *common.Hash
			if string(req.Params[2]) != "null" {
				var root common.Hash
				if err := json.Unmarshal(req.Params[2], &root); err != nil {
					respErr = &JsonRPCError{Code: InvalidParams, Message: "invalid parent beacon block root"}
					break
				}
				parentBeaconBlockRoot = &root
			}
			respData, respErr = m.behavior.HandleNewPayloadV4(
				m.state,
				payload,
				versionedHashes,
				parentBeaconBlockRoot,
				nil,
			)
		case "engine_newPayloadV5":
			// Delegate to V4 handler (same params).
			if len(req.Params) < 3 {
				respErr = &JsonRPCError{Code: InvalidParams, Message: "invalid params"}
				break
			}
			var payload engine.ExecutableData
			if err := json.Unmarshal(req.Params[0], &payload); err != nil {
				respErr = &JsonRPCError{Code: InvalidParams, Message: "invalid payload"}
				break
			}
			var versionedHashes []common.Hash
			if err := json.Unmarshal(req.Params[1], &versionedHashes); err != nil {
				respErr = &JsonRPCError{Code: InvalidParams, Message: "invalid versioned hashes"}
				break
			}
			var parentBeaconBlockRoot *common.Hash
			if string(req.Params[2]) != "null" {
				var root common.Hash
				if err := json.Unmarshal(req.Params[2], &root); err != nil {
					respErr = &JsonRPCError{Code: InvalidParams, Message: "invalid parent beacon block root"}
					break
				}
				parentBeaconBlockRoot = &root
			}
			respData, respErr = m.behavior.HandleNewPayloadV4(
				m.state,
				payload,
				versionedHashes,
				parentBeaconBlockRoot,
				nil,
			)
		case "engine_forkchoiceUpdatedV3":
			if len(req.Params) < 1 {
				respErr = &JsonRPCError{Code: InvalidParams, Message: "invalid params"}
				break
			}
			var forkchoiceState engine.ForkchoiceStateV1
			if err := json.Unmarshal(req.Params[0], &forkchoiceState); err != nil {
				respErr = &JsonRPCError{Code: InvalidForkchoiceState, Message: "invalid forkchoice state"}
				break
			}

			var payloadAttributes *engine.PayloadAttributes
			if len(req.Params) > 1 && string(req.Params[1]) != "null" {
				if err := json.Unmarshal(req.Params[1], &payloadAttributes); err != nil {
					respErr = &JsonRPCError{Code: InvalidPayloadAttributes, Message: "invalid payload attributes"}
					break
				}
			}
			respData, respErr = m.behavior.HandleForkchoiceUpdatedV3(m.state, forkchoiceState, payloadAttributes)
		case "engine_getPayloadV4":
			if len(req.Params) < 1 {
				respErr = &JsonRPCError{Code: InvalidParams, Message: "invalid params"}
				break
			}
			var payloadID engine.PayloadID
			if err := json.Unmarshal(req.Params[0], &payloadID); err != nil {
				respErr = &JsonRPCError{Code: InvalidPayloadID, Message: "invalid payload id"}
				break
			}
			respData, respErr = m.behavior.HandleGetPayloadV4(m.state, payloadID)
		case "engine_getPayloadV5":
			if len(req.Params) < 1 {
				respErr = &JsonRPCError{Code: InvalidParams, Message: "invalid params"}
				break
			}
			var payloadID engine.PayloadID
			if err := json.Unmarshal(req.Params[0], &payloadID); err != nil {
				respErr = &JsonRPCError{Code: InvalidPayloadID, Message: "invalid payload id"}
				break
			}
			respData, respErr = m.behavior.HandleGetPayloadV5(m.state, payloadID)
		default:
			respData = make(map[string]interface{})
		}

		var resp map[string]interface{}
		if respErr != nil {
			resp = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error":   respErr,
			}
		} else {
			resp = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  respData,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	m.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	return m
}

// Start runs the mock engine's HTTP server in a goroutine.
// Returns an error if the port is already in use.
func (m *MockEngine) Start() error {
	// Check for port conflicts before starting
	if err := m.checkPortConflicts(); err != nil {
		return err
	}

	// Start the server in a goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := m.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Give the server a moment to start and check for immediate errors
	select {
	case err := <-errChan:
		// Check if it's a port-in-use error and provide a clearer message
		if netErr, ok := err.(*net.OpError); ok && netErr.Op == "listen" {
			return fmt.Errorf(
				"port %s is already in use (check for Docker containers or other services): %w",
				m.server.Addr, err,
			)
		}
		return fmt.Errorf("failed to start mock engine: %w", err)
	case <-time.After(100 * time.Millisecond):
		// Server started successfully
		return nil
	}
}

// checkPortConflicts checks if any process is using the port, including cross-protocol conflicts
func (m *MockEngine) checkPortConflicts() error {
	// Try to bind to the exact same address the HTTP server will use
	listener, err := net.Listen("tcp", m.server.Addr)
	if err != nil {
		return fmt.Errorf("port %s is already in use (possibly by Docker or another service): %w", m.server.Addr, err)
	}
	listener.Close()

	// Also check if there's any service responding on localhost:port that might interfere
	// This catches cases where Docker binds to IPv6 but localhost resolution goes there first
	host, port, err := net.SplitHostPort(m.server.Addr)
	if err != nil {
		return fmt.Errorf("invalid server address %s: %w", m.server.Addr, err)
	}

	if host == "localhost" {
		// Test if anything is already responding on localhost:port
		conn, err := net.DialTimeout("tcp", m.server.Addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return fmt.Errorf(
				"port %s is already in use by another service (detected active connection, possibly Docker on IPv6): "+
					"check 'lsof -i :%s'",
				m.server.Addr, port,
			)
		}
	}

	return nil
}

// Stop gracefully shuts down the mock engine's HTTP server.
func (m *MockEngine) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return m.server.Shutdown(ctx)
}

// WaitUntilReady blocks until the mock engine's HTTP server is ready to accept connections.
func (m *MockEngine) WaitUntilReady() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	for {
		// Attempt to connect
		req, err := http.NewRequestWithContext(ctx, "GET", "http://"+m.server.Addr+"/", nil)
		if err == nil {
			client := &http.Client{Timeout: 100 * time.Millisecond}
			_, err = client.Do(req)
			if err == nil {
				// Server is up and responding
				return
			}
		}

		// If the context is done, we've timed out
		if ctx.Err() != nil {
			panic(fmt.Sprintf("mock engine at %s failed to become ready in time: %v", m.server.Addr, ctx.Err()))
		}

		// Wait a bit before retrying
		time.Sleep(10 * time.Millisecond)
	}
}

func (m *MockEngine) GetLastPayload() *engine.ExecutionPayloadEnvelope {
	m.state.Mu.RLock()
	defer m.state.Mu.RUnlock()
	return m.state.LastPayload
}

// GetRequests returns a copy of all requests received by the mock engine.
func (m *MockEngine) GetRequests() []RecordedRequest {
	m.state.Mu.RLock()
	defer m.state.Mu.RUnlock()
	// Return a copy to prevent race conditions on the slice
	requestsCopy := make([]RecordedRequest, len(m.state.Requests))
	copy(requestsCopy, m.state.Requests)
	return requestsCopy
}
