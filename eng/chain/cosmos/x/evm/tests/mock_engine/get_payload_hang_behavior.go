package mock_engine

import (
	"time"

	"github.com/ethereum/go-ethereum/beacon/engine"
)

// GetPayloadHangBehavior implements the EngineBehavior interface and provides a behavior
// that hangs for a few seconds before returning for get payload.
type GetPayloadHangBehavior struct {
	DefaultEngineBehavior
}

// HandleGetPayload implements the EngineBehavior interface.
func (b *GetPayloadHangBehavior) HandleGetPayloadV4(
	state *EngineState,
	payloadID engine.PayloadID,
) (engine.ExecutionPayloadEnvelope, *JsonRPCError) {
	time.Sleep(1 * time.Second)
	return b.DefaultEngineBehavior.HandleGetPayloadV4(state, payloadID)
}

func (b *GetPayloadHangBehavior) HandleGetPayloadV5(
	state *EngineState,
	payloadID engine.PayloadID,
) (engine.ExecutionPayloadEnvelope, *JsonRPCError) {
	return b.HandleGetPayloadV4(state, payloadID)
}
