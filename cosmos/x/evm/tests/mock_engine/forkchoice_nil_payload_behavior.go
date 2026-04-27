package mock_engine

import (
	"github.com/ethereum/go-ethereum/beacon/engine"
)

// ForkchoiceNilPayloadBehavior implements the EngineBehavior interface and provides a behavior
// that returns a nil payload.
type ForkchoiceNilPayloadBehavior struct {
	DefaultEngineBehavior
}

// HandleForkchoiceUpdated implements the EngineBehavior interface.
func (b *ForkchoiceNilPayloadBehavior) HandleForkchoiceUpdatedV3(
	state *EngineState,
	forkchoiceState engine.ForkchoiceStateV1,
	payloadAttributes *engine.PayloadAttributes,
) (engine.ForkChoiceResponse, *JsonRPCError) {
	respData := engine.ForkChoiceResponse{
		PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID},
		PayloadID:     nil,
	}
	return respData, nil
}
