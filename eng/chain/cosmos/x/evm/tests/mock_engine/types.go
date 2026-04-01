package mock_engine

import (
	"encoding/json"
	"sync"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"

	"nexus/x/evm/types"
)

// EngineState holds the state of the mock engine that can be manipulated by behaviors.
type EngineState struct {
	Mu          sync.RWMutex
	Payloads    map[engine.PayloadID]*engine.ExecutionPayloadEnvelope
	LastPayload *engine.ExecutionPayloadEnvelope
	Requests    []RecordedRequest
}

// EngineBehavior defines the request handling logic for a mock engine.
// Different implementations can be used to simulate different scenarios.
type EngineBehavior interface {
	HandleNewPayloadV4(
		state *EngineState,
		payload engine.ExecutableData,
		versionedHashes []common.Hash,
		parentBeaconBlockRoot *common.Hash,
		requests *types.ConsensusRequests,
	) (engine.PayloadStatusV1, *JsonRPCError)
	HandleForkchoiceUpdatedV3(
		state *EngineState,
		forkchoiceState engine.ForkchoiceStateV1,
		payloadAttributes *engine.PayloadAttributes,
	) (engine.ForkChoiceResponse, *JsonRPCError)
	HandleGetPayloadV4(state *EngineState, payloadID engine.PayloadID) (engine.ExecutionPayloadEnvelope, *JsonRPCError)
}

// RecordedRequest captures the details of a JSON-RPC request for inspection in tests.
type RecordedRequest struct {
	Method string
	Params []json.RawMessage
}

type JsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *JsonRPCError) Error() string {
	return e.Message
}

const (
	ErrorUnknownPayload           = -38001
	ErrorForkchoiceUpdateFailure1 = -38002
	ErrorForkchoiceUpdateFailure2 = -38003
	ErrorForkchoiceUpdateFailure3 = -38004
	ErrorGetPayloadFailure1       = -38005
	ErrorGetPayloadFailure2       = -38006
	ErrorGetPayloadFailure3       = -38007
	InvalidParams                 = -32602
	InvalidForkchoiceState        = -32603
	InvalidPayloadAttributes      = -32604
	InvalidPayloadID              = -32605
)
