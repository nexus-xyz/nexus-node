# Background

This document maps the concepts in [`README.md`](./README.md) — the CL/EL
split, ABCI++ ↔ Engine API translation, and Cosmos SDK setup — to concrete
files in this codebase.

# Most important files

- The most important files are the following:
  - `lib/engine.go` - EngineAPI
  - `x/evm/keeper/keeper.go` - Client state
  - `x/evm/keeper/msg_server.go` - `ExecutionPayload` message handler
  - `x/evm/keeper/proposal_server.go` - Proposal handler
  - `x/evm/keeper/abci.go` - ABCI++
  - `app/proposal.go` - `ProcessProposal`

# Notes on ABCI++ Translations to EngineAPI

- Although the [ABCI++ v0.38 spec](https://docs.cometbft.com/v0.38/spec/abci/abci++_comet_expected_behavior) defines a
  full round of methods as `PrepareProposal` -> `ProcessProposal` -> `ExtendVote` -> `VerifyVoteExtension` ->
  `FinalizeBlock` -> `Commit`, this codebase does not override all methods:
  - `ExtendVote`, `VerifyVoteExtension`, and `Commit` are not overridden and follow the default implementation.

- `PrepareProposal` is implemented in `x/evm/keeper/abci.go`. It retries until the following path succeeds or times out:
  1. Sends `ForkchoiceUpdatedV3` to EngineAPI
  2. Sends `GetPayloadV3` to EngineAPI (with payload id from previous response)
  3. Builds a cosmos block that contains a single cosmos transaction that contains the EVM block execution payload that
     the execution layer sent via EngineAPI

- `ProcessProposal` is implemented in `app/proposal.go`. It uses a custom handler that validates the transaction
  containing the execution payload.

- The logic that would normally be in `FinalizeBlock` is handled within `x/evm/keeper/msg_server.go`'s
  `ExecutionPayload` message handler. This handler is invoked when a block is being finalized and contains a
  `MsgExecutionPayload` transaction. The handler ensures it is running in `ExecModeFinalize`.
