package lib

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"nexus/x/evm/types"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	DefaultTimeout      = 10 * time.Second
	newPayloadV3        = "engine_newPayloadV3"
	newPayloadV4        = "engine_newPayloadV4"
	forkchoiceUpdatedV3 = "engine_forkchoiceUpdatedV3"
	getPayloadV3        = "engine_getPayloadV3"
	getPayloadV4        = "engine_getPayloadV4"
)

type EngineClient interface {
	// NewPayloadV3 informs the engine that a new payload has been created.
	NewPayloadV3(
		ctx context.Context,
		params engine.ExecutableData,
		versionedHashes []common.Hash,
		beaconRoot *common.Hash,
	) (engine.PayloadStatusV1, error)

	// NewPayloadV4 informs the engine that a new payload has been created.
	NewPayloadV4(
		ctx context.Context,
		params engine.ExecutableData,
		versionedHashes []common.Hash,
		beaconRoot *common.Hash,
		requests *types.ConsensusRequests,
	) (engine.PayloadStatusV1, error)

	// ForkchoiceUpdatedV3 informs the engine that the fork has been updated.
	ForkchoiceUpdatedV3(
		ctx context.Context,
		update engine.ForkchoiceStateV1,
		payloadAttributes *engine.PayloadAttributes,
	) (engine.ForkChoiceResponse, error)

	// GetPayloadV3 requests a cached payload from the engine.
	GetPayloadV3(ctx context.Context, payloadID engine.PayloadID) (*engine.ExecutionPayloadEnvelope, error)

	// GetPayloadV4 requests a cached payload from the engine.
	GetPayloadV4(ctx context.Context, payloadID engine.PayloadID) (*engine.ExecutionPayloadEnvelope, error)
}

type engineClient struct {
	rpcClient *rpc.Client
}

// NewAuthClient creates a new engine client that uses JWT authentication.
func NewAuthClient(ctx context.Context, url string, jwtSecret *JwtSecret) (EngineClient, error) {
	transport := NewJwtClient(http.DefaultTransport, jwtSecret)
	client := &http.Client{
		Timeout:   DefaultTimeout,
		Transport: transport,
	}

	rpcClient, err := rpc.DialOptions(ctx, url, rpc.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("dial rpc: %w", err)
	}

	return &engineClient{
		rpcClient: rpcClient,
	}, nil
}

func (c *engineClient) NewPayloadV3(
	ctx context.Context,
	params engine.ExecutableData,
	versionedHashes []common.Hash,
	beaconRoot *common.Hash,
) (engine.PayloadStatusV1, error) {
	var response engine.PayloadStatusV1

	err := c.rpcClient.CallContext(ctx, &response, newPayloadV3, params, versionedHashes, beaconRoot)
	if err == nil {
		return response, nil
	}

	return engine.PayloadStatusV1{}, fmt.Errorf("new payload v3: %w, response: %v", err, response)
}

func (c *engineClient) NewPayloadV4(
	ctx context.Context,
	params engine.ExecutableData,
	versionedHashes []common.Hash,
	beaconRoot *common.Hash,
	requests *types.ConsensusRequests,
) (engine.PayloadStatusV1, error) {
	var response engine.PayloadStatusV1

	requestsData := make([]hexutil.Bytes, 0)
	if requests != nil {
		requestsData = requests.Encode()
	}

	err := c.rpcClient.CallContext(ctx, &response, newPayloadV4, params, versionedHashes, beaconRoot, requestsData)
	if err == nil {
		return response, nil
	}

	return engine.PayloadStatusV1{}, fmt.Errorf("new payload v4: %w, response: %v", err, response)
}

func (c *engineClient) ForkchoiceUpdatedV3(
	ctx context.Context,
	update engine.ForkchoiceStateV1,
	payloadAttributes *engine.PayloadAttributes,
) (engine.ForkChoiceResponse, error) {
	var response engine.ForkChoiceResponse
	err := c.rpcClient.CallContext(ctx, &response, forkchoiceUpdatedV3, update, payloadAttributes)
	if err == nil {
		switch response.PayloadStatus.Status {
		case "ACCEPTED":
			return engine.ForkChoiceResponse{}, fmt.Errorf("unexpected status accepted: %v", response)
		default:
			return response, nil
		}
	}

	return engine.ForkChoiceResponse{}, fmt.Errorf("forkchoice updated v3: %w, response: %v", err, response)
}

func (c *engineClient) GetPayloadV3(
	ctx context.Context,
	payloadID engine.PayloadID,
) (*engine.ExecutionPayloadEnvelope, error) {
	var response engine.ExecutionPayloadEnvelope
	err := c.rpcClient.CallContext(ctx, &response, getPayloadV3, payloadID)
	if err == nil {
		return &response, nil
	}

	return nil, fmt.Errorf("get payload v3: %w, response: %v", err, response)
}

func (c *engineClient) GetPayloadV4(
	ctx context.Context,
	payloadID engine.PayloadID,
) (*engine.ExecutionPayloadEnvelope, error) {
	var response engine.ExecutionPayloadEnvelope
	err := c.rpcClient.CallContext(ctx, &response, getPayloadV4, payloadID)
	if err == nil {
		return &response, nil
	}

	return nil, fmt.Errorf("get payload v4: %w, response: %v", err, response)
}

// stubEngineClient is a no-op engine client used when JWT is not configured.
// It returns SYNCING status for all operations, indicating the engine is not available.
type stubEngineClient struct{}

// NewStubEngineClient creates a stub engine client that returns SYNCING for all calls.
func NewStubEngineClient() EngineClient {
	return &stubEngineClient{}
}

func (s *stubEngineClient) NewPayloadV3(
	_ context.Context,
	_ engine.ExecutableData,
	_ []common.Hash,
	_ *common.Hash,
) (engine.PayloadStatusV1, error) {
	return engine.PayloadStatusV1{Status: engine.SYNCING}, nil
}

func (s *stubEngineClient) NewPayloadV4(
	_ context.Context,
	_ engine.ExecutableData,
	_ []common.Hash,
	_ *common.Hash,
	_ *types.ConsensusRequests,
) (engine.PayloadStatusV1, error) {
	return engine.PayloadStatusV1{Status: engine.SYNCING}, nil
}

func (s *stubEngineClient) ForkchoiceUpdatedV3(
	_ context.Context,
	_ engine.ForkchoiceStateV1,
	_ *engine.PayloadAttributes,
) (engine.ForkChoiceResponse, error) {
	return engine.ForkChoiceResponse{
		PayloadStatus: engine.PayloadStatusV1{Status: engine.SYNCING},
	}, nil
}

func (s *stubEngineClient) GetPayloadV3(
	_ context.Context,
	_ engine.PayloadID,
) (*engine.ExecutionPayloadEnvelope, error) {
	return nil, fmt.Errorf("EVM not enabled (set EVM_ENABLED=true to enable)")
}

func (s *stubEngineClient) GetPayloadV4(
	_ context.Context,
	_ engine.PayloadID,
) (*engine.ExecutionPayloadEnvelope, error) {
	return nil, fmt.Errorf("EVM not enabled (set EVM_ENABLED=true to enable)")
}
