package mock_engine

import (
	"github.com/ethereum/go-ethereum/beacon/engine"
)

// UnknownPayloadBehavior implements the EngineBehavior interface and provides a behavior
// that returns an unknown payload.
type UnknownPayloadBehavior struct {
	DefaultEngineBehavior
}

// HandleGetPayload implements the EngineBehavior interface.
func (b *UnknownPayloadBehavior) HandleGetPayloadV4(
	state *EngineState,
	payloadID engine.PayloadID,
) (engine.ExecutionPayloadEnvelope, *JsonRPCError) {
	return engine.ExecutionPayloadEnvelope{}, &JsonRPCError{
		Code:    ErrorUnknownPayload,
		Message: engine.UnknownPayload.Error(),
	}
}
