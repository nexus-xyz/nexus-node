package keeper

import (
	"context"
	"fmt"

	"github.com/cosmos/cosmos-sdk/baseapp"

	"nexus/x/evm/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// RegisterProposalHandlers registers custom handlers for ProcessProposal.
func (k *Keeper) RegisterProposalHandlers(router *baseapp.MsgServiceRouter) {
	proposalServer := &proposalMsgServer{Keeper: *k}
	types.RegisterMsgServer(router, proposalServer)
}

// proposalMsgServer is a custom message server for ProcessProposal that
// validates the execution payload but does not update the block state.
type proposalMsgServer struct {
	Keeper
}

// ExecutionPayload validates the execution payload but does not update
// the block state.
func (s *proposalMsgServer) ExecutionPayload(ctx context.Context, req *types.MsgExecutionPayload) (
	*types.MsgExecutionPayloadResponse, error,
) {
	state, err := s.GetBlockState(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get block state: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Validate the payload
	_, err = s.Keeper.validatePayload(sdkCtx, req, &state)
	if err != nil {
		return nil, err
	}

	return &types.MsgExecutionPayloadResponse{}, nil
}

// UpdateParams is required to implement the MsgServer interface
func (s *proposalMsgServer) UpdateParams(
	ctx context.Context,
	req *types.MsgUpdateParams,
) (*types.MsgUpdateParamsResponse, error) {
	return nil, fmt.Errorf("UpdateParams should not be called during ProcessProposal")
}
