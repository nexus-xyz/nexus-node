package mock_engine

import (
	"github.com/ethereum/go-ethereum/beacon/engine"
)

// ForkchoiceInvalidStatusBehavior implements the EngineBehavior interface and provides a behavior
// that returns an invalid status.
type ForkchoiceInvalidStatusBehavior struct {
	DefaultEngineBehavior
}

// HandleForkchoiceUpdated implements the EngineBehavior interface.
func (b *ForkchoiceInvalidStatusBehavior) HandleForkchoiceUpdatedV3(
	state *EngineState,
	forkchoiceState engine.ForkchoiceStateV1,
	payloadAttributes *engine.PayloadAttributes,
) (engine.ForkChoiceResponse, *JsonRPCError) {
	resp, err := b.DefaultEngineBehavior.HandleForkchoiceUpdatedV3(state, forkchoiceState, payloadAttributes)
	if err != nil {
		return engine.ForkChoiceResponse{}, err
	}
	resp.PayloadStatus.Status = engine.INVALID
	return resp, nil
}
