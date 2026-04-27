package mock_engine

import (
	"math/big"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"

	"nexus/x/evm/types"
)

var mockPayloadID = engine.PayloadID{1, 2, 3, 4, 5, 6, 7, 8}

// DefaultEngineBehavior provides a "happy path" implementation for the mock engine. It generates a valid payload upon request.
// It is intended to be embedded in other EngineBehavior implementations to provide default behavior.
type DefaultEngineBehavior struct{}

// HandleNewPayload implements the EngineBehavior interface.
func (b *DefaultEngineBehavior) HandleNewPayloadV4(
	state *EngineState,
	payload engine.ExecutableData,
	versionedHashes []common.Hash,
	parentBeaconBlockRoot *common.Hash,
	requests *types.ConsensusRequests,
) (engine.PayloadStatusV1, *JsonRPCError) {
	// For the default behavior, just return VALID status
	return engine.PayloadStatusV1{Status: engine.VALID}, nil
}

// HandleForkchoiceUpdated implements the EngineBehavior interface.
func (b *DefaultEngineBehavior) HandleForkchoiceUpdatedV3(
	state *EngineState,
	forkchoiceState engine.ForkchoiceStateV1,
	payloadAttributes *engine.PayloadAttributes,
) (engine.ForkChoiceResponse, *JsonRPCError) {
	state.Mu.Lock()
	defer state.Mu.Unlock()

	payloadID := mockPayloadID
	payload := newDefaultPayload(payloadAttributes)
	envelope := newDefaultEnvelope(payload)
	state.Payloads[payloadID] = envelope
	state.LastPayload = envelope

	respData := engine.ForkChoiceResponse{
		PayloadStatus: engine.PayloadStatusV1{Status: engine.VALID},
		PayloadID:     &payloadID,
	}
	return respData, nil
}

// HandleGetPayload implements the EngineBehavior interface.
func (b *DefaultEngineBehavior) HandleGetPayloadV4(
	state *EngineState,
	payloadID engine.PayloadID,
) (engine.ExecutionPayloadEnvelope, *JsonRPCError) {
	state.Mu.RLock()
	defer state.Mu.RUnlock()

	if payload, ok := state.Payloads[payloadID]; ok {
		return *payload, nil
	}
	return engine.ExecutionPayloadEnvelope{}, &JsonRPCError{
		Code:    ErrorUnknownPayload,
		Message: engine.UnknownPayload.Error(),
	}
}

func (b *DefaultEngineBehavior) HandleGetPayloadV5(
	state *EngineState,
	payloadID engine.PayloadID,
) (engine.ExecutionPayloadEnvelope, *JsonRPCError) {
	return b.HandleGetPayloadV4(state, payloadID)
}

func newDefaultPayload(payloadAttributes *engine.PayloadAttributes) *engine.ExecutableData {
	return &engine.ExecutableData{
		ParentHash:       common.HexToHash("0xabcdef01"),
		FeeRecipient:     payloadAttributes.SuggestedFeeRecipient,
		StateRoot:        common.HexToHash("0xabcdef02"),
		ReceiptsRoot:     common.HexToHash("0xabcdef03"),
		LogsBloom:        make([]byte, 256),
		Random:           payloadAttributes.Random,
		Number:           1,
		GasLimit:         30000000,
		GasUsed:          21000,
		Timestamp:        payloadAttributes.Timestamp,
		ExtraData:        []byte("mock extra data"),
		BaseFeePerGas:    big.NewInt(1000000000),
		BlockHash:        common.HexToHash("0xdeadbeef"),
		Transactions:     [][]byte{},
		Withdrawals:      payloadAttributes.Withdrawals,
		BlobGasUsed:      new(uint64),
		ExcessBlobGas:    new(uint64),
		ExecutionWitness: nil,
	}
}

func newDefaultEnvelope(payload *engine.ExecutableData) *engine.ExecutionPayloadEnvelope {
	return &engine.ExecutionPayloadEnvelope{
		ExecutionPayload: payload,
		BlockValue:       big.NewInt(0),
		BlobsBundle:      nil,
		Requests:         [][]byte{},
		Override:         false,
		Witness:          nil,
	}
}
