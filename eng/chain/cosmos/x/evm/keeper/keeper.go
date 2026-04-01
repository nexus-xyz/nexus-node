package keeper

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/grpc"
	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"

	nexus "nexus/app/types"
	"nexus/lib"
	"nexus/x/evm/types"
)

type rawUseBlockTimestamp struct {
	StartBlockHeight string `yaml:"start_block_height"`
	Offset           string `yaml:"offset"`
	StopBlockHeight  string `yaml:"stop_block_height"`
}

type rawConfig struct {
	SuggestedFeeRecipient string                `yaml:"suggested_fee_recipient"`
	UseBlockTimestamp     *rawUseBlockTimestamp `yaml:"use_block_timestamp,omitempty"`
	UnixTimestampOffset   string                `yaml:"unix_timestamp_offset"`
}

type UseBlockTimestamp struct {
	StartBlockHeight uint64
	Offset           uint64
	StopBlockHeight  *uint64
}

type Config struct {
	SuggestedFeeRecipient common.Address
	UseBlockTimestamp     *UseBlockTimestamp
	UnixTimestampOffset   uint64
}

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority []byte

	Schema     collections.Schema
	Params     collections.Item[types.Params]
	BlockState collections.Item[types.BlockState]

	engineClient lib.EngineClient
	txConfig     client.TxConfig

	SuggestedFeeRecipient common.Address
	UseBlockTimestamp     *UseBlockTimestamp
	UnixTimestampOffset   uint64

	chainSpec nexus.ChainSpec
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,
	txConfig client.TxConfig,
	chainSpec nexus.ChainSpec,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)

	engineClient, err := createEngineClient(context.Background())
	if err != nil {
		panic(err)
	}

	config := readConfig()

	k := Keeper{
		storeService:          storeService,
		cdc:                   cdc,
		addressCodec:          addressCodec,
		authority:             authority,
		engineClient:          engineClient,
		txConfig:              txConfig,
		Params:                collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		BlockState:            collections.NewItem(sb, types.BlockStateKey, "block_state", types.BlockStateValue{}),
		SuggestedFeeRecipient: config.SuggestedFeeRecipient,
		UseBlockTimestamp:     config.UseBlockTimestamp,
		UnixTimestampOffset:   config.UnixTimestampOffset,
		chainSpec:             chainSpec,
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() []byte {
	return k.authority
}

// GetBlockState returns the current block state
func (k Keeper) GetBlockState(ctx context.Context) (types.BlockState, error) {
	return k.BlockState.Get(ctx)
}

// SetBlockState sets the current block state
func (k Keeper) SetBlockState(ctx context.Context, blockState types.BlockState) error {
	return k.BlockState.Set(ctx, blockState)
}

// HasBlockState checks if block state exists
func (k Keeper) HasBlockState(ctx context.Context) (bool, error) {
	return k.BlockState.Has(ctx)
}

// CalculateTimestamp calculates the block timestamp based on the configuration.
//
// Between start_block_height and stop_block_height: blockHeight + offset.
// Otherwise: UNIX wall-clock + unix_timestamp_offset, where unix_timestamp_offset
// is 0 below start_block_height and the configured value above stop_block_height.
// Falls back to previousTimestamp+1 if the result is not strictly greater.
func (k Keeper) CalculateTimestamp(sdkCtx sdk.Context, blockHeight uint64, previousTimestamp uint64) uint64 {
	if k.UseBlockTimestamp == nil {
		timestamp := uint64(sdkCtx.BlockTime().Unix())
		if timestamp <= previousTimestamp {
			timestamp = previousTimestamp + 1
		}
		return timestamp
	}

	cfg := k.UseBlockTimestamp

	if blockHeight >= cfg.StartBlockHeight && (cfg.StopBlockHeight == nil || blockHeight < *cfg.StopBlockHeight) {
		return blockHeight + cfg.Offset
	}

	unixOffset := uint64(0)
	if cfg.StopBlockHeight != nil && blockHeight >= *cfg.StopBlockHeight {
		unixOffset = k.UnixTimestampOffset
	}

	timestamp := uint64(sdkCtx.BlockTime().Unix()) + unixOffset
	if timestamp <= previousTimestamp {
		timestamp = previousTimestamp + 1
	}
	return timestamp
}

// readConfig returns a config for the keeper
func readConfig() Config {
	data, err := lib.ReadConfigFile()
	if err != nil {
		return Config{}
	}

	var config rawConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		panic(fmt.Errorf("failed to decode config %w", err))
	}

	recipientStr := config.SuggestedFeeRecipient
	var recipient common.Address

	if recipientStr != "" {
		if !common.IsHexAddress(recipientStr) {
			panic(fmt.Errorf("could not decode recipient as hex: %v", recipientStr))
		}
		recipient = common.HexToAddress(recipientStr)
	}

	var useBlockTimestamp *UseBlockTimestamp
	if config.UseBlockTimestamp != nil {
		offsetVal := uint64(0)
		if config.UseBlockTimestamp.Offset != "" {
			var err error
			offsetVal, err = strconv.ParseUint(config.UseBlockTimestamp.Offset, 10, 64)
			if err != nil {
				panic(fmt.Errorf(
					"could not decode use_block_timestamp.offset as int: %v, %w",
					config.UseBlockTimestamp.Offset, err))
			}
		}

		startBlockHeightVal := uint64(0)
		if config.UseBlockTimestamp.StartBlockHeight != "" {
			var err error
			startBlockHeightVal, err = strconv.ParseUint(config.UseBlockTimestamp.StartBlockHeight, 10, 64)
			if err != nil {
				panic(fmt.Errorf(
					"could not decode use_block_timestamp.start_block_height as int: %v, %w",
					config.UseBlockTimestamp.StartBlockHeight, err))
			}
		}

		stopBlockHeightVal := uint64(0)
		if config.UseBlockTimestamp.StopBlockHeight != "" {
			var err error
			stopBlockHeightVal, err = strconv.ParseUint(config.UseBlockTimestamp.StopBlockHeight, 10, 64)
			if err != nil {
				panic(fmt.Errorf(
					"could not decode use_block_timestamp.stop_block_height as int: %v, %w",
					config.UseBlockTimestamp.StopBlockHeight, err))
			}
		}

		var stopBlockHeightPtr *uint64
		if config.UseBlockTimestamp.StopBlockHeight != "" {
			if stopBlockHeightVal < startBlockHeightVal {
				panic(fmt.Errorf(
					"use_block_timestamp.stop_block_height (%d) must be >= start_block_height (%d)",
					stopBlockHeightVal, startBlockHeightVal))
			}
			stopBlockHeightPtr = &stopBlockHeightVal
		}

		ubt := UseBlockTimestamp{
			StartBlockHeight: startBlockHeightVal,
			Offset:           offsetVal,
			StopBlockHeight:  stopBlockHeightPtr,
		}
		useBlockTimestamp = &ubt
	}

	unixTimestampOffsetVal := uint64(0)
	if config.UnixTimestampOffset != "" {
		var err error
		unixTimestampOffsetVal, err = strconv.ParseUint(config.UnixTimestampOffset, 10, 64)
		if err != nil {
			panic(fmt.Errorf(
				"could not decode unix_timestamp_offset as int: %v, %w",
				config.UnixTimestampOffset, err))
		}
	}

	return Config{
		SuggestedFeeRecipient: recipient,
		UseBlockTimestamp:     useBlockTimestamp,
		UnixTimestampOffset:   unixTimestampOffsetVal,
	}
}

// createEngineClient creates an engineClient to interface with the EVM.
func createEngineClient(ctx context.Context) (lib.EngineClient, error) {
	jwtSecretPath := "jwt.hex"
	if envPath := os.Getenv("EVM_ENGINE_JWT_SECRET_PATH"); envPath != "" {
		jwtSecretPath = envPath
	}

	engineURL := "http://localhost:8551"
	if envURL := os.Getenv("EVM_ENGINE_URL"); envURL != "" {
		engineURL = envURL
	}

	// Load JWT secret from file
	jwtSecret, err := lib.LoadJWTSecret(jwtSecretPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load JWT secret from %s: %w", jwtSecretPath, err)
	}

	// Create authenticated engine client
	engineClient, err := lib.NewAuthClient(ctx, engineURL, jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create engine client for %s: %w", engineURL, err)
	}

	return engineClient, nil
}

func (k Keeper) RegisterProposalService(server grpc.Server) {
	types.RegisterMsgServer(server, msgServer{k})
}
