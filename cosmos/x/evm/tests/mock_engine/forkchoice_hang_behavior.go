package mock_engine

import (
	"time"

	"github.com/ethereum/go-ethereum/beacon/engine"
)

// ForkchoiceHangBehavior implements the EngineBehavior interface and provides a behavior
// that hangs for a few seconds before returning for forkchoice updates.
type ForkchoiceHangBehavior struct {
	DefaultEngineBehavior
}

// HandleForkchoiceUpdated implements the EngineBehavior interface.
func (b *ForkchoiceHangBehavior) HandleForkchoiceUpdatedV3(
	state *EngineState,
	forkchoiceState engine.ForkchoiceStateV1,
	payloadAttributes *engine.PayloadAttributes,
) (engine.ForkChoiceResponse, *JsonRPCError) {
	time.Sleep(1 * time.Second)

	return b.DefaultEngineBehavior.HandleForkchoiceUpdatedV3(state, forkchoiceState, payloadAttributes)
}
