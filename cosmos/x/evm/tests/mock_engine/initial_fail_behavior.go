package mock_engine

import (
	"sync"

	"github.com/ethereum/go-ethereum/beacon/engine"
)

// InitialFailBehavior implements the EngineBehavior interface and provides a behavior
// that fails the first three times for both HandleForkchoiceUpdated and HandleGetPayload, but then succeeds.
type InitialFailBehavior struct {
	Mu                 sync.Mutex
	ForkchoiceAttempts int
	GetPayloadAttempts int
	DefaultEngineBehavior
}

// HandleForkchoiceUpdated implements the EngineBehavior interface.
func (b *InitialFailBehavior) HandleForkchoiceUpdatedV3(
	state *EngineState,
	forkchoiceState engine.ForkchoiceStateV1,
	payloadAttributes *engine.PayloadAttributes,
) (engine.ForkChoiceResponse, *JsonRPCError) {
	b.Mu.Lock()
	b.ForkchoiceAttempts++
	attempt := b.ForkchoiceAttempts
	b.Mu.Unlock()

	switch attempt {
	case 1:
		return engine.ForkChoiceResponse{}, &JsonRPCError{
			Code:    ErrorForkchoiceUpdateFailure1,
			Message: "Test forkchoice update failure: first attempt",
		}
	case 2:
		return engine.ForkChoiceResponse{}, &JsonRPCError{
			Code:    ErrorForkchoiceUpdateFailure2,
			Message: "Test forkchoice update failure: second attempt",
		}
	case 3:
		return engine.ForkChoiceResponse{}, &JsonRPCError{
			Code:    ErrorForkchoiceUpdateFailure3,
			Message: "Test forkchoice update failure: third attempt",
		}
	}

	return b.DefaultEngineBehavior.HandleForkchoiceUpdatedV3(state, forkchoiceState, payloadAttributes)
}

// HandleGetPayload implements the EngineBehavior interface.
func (b *InitialFailBehavior) HandleGetPayloadV4(
	state *EngineState,
	payloadID engine.PayloadID,
) (engine.ExecutionPayloadEnvelope, *JsonRPCError) {
	b.Mu.Lock()
	b.GetPayloadAttempts++
	attempt := b.GetPayloadAttempts
	b.Mu.Unlock()

	switch attempt {
	case 1:
		return engine.ExecutionPayloadEnvelope{}, &JsonRPCError{
			Code:    ErrorGetPayloadFailure1,
			Message: "Test get payload failure: first attempt",
		}
	case 2:
		return engine.ExecutionPayloadEnvelope{}, &JsonRPCError{
			Code:    ErrorGetPayloadFailure2,
			Message: "Test get payload failure: second attempt",
		}
	case 3:
		return engine.ExecutionPayloadEnvelope{}, &JsonRPCError{
			Code:    ErrorGetPayloadFailure3,
			Message: "Test get payload failure: third attempt",
		}
	}

	return b.DefaultEngineBehavior.HandleGetPayloadV4(state, payloadID)
}

func (b *InitialFailBehavior) HandleGetPayloadV5(
	state *EngineState,
	payloadID engine.PayloadID,
) (engine.ExecutionPayloadEnvelope, *JsonRPCError) {
	return b.HandleGetPayloadV4(state, payloadID)
}
