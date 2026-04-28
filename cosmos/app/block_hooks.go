package app

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"gopkg.in/yaml.v3"

	"nexus/lib"
)

// hookType represents the hook action to perform.
type hookType string

const (
	hookUnjail                hookType = "unjail"
	hookAddValidator          hookType = "add_validator"
	hookRemoveValidator       hookType = "remove_validator"
	hookReconcileStakingPools hookType = "reconcile_staking_pools"
)

type unjailParams struct {
	validator    string
	targetTokens sdkmath.Int // zero value means no token adjustment
}

type addValidatorParams struct {
	validator string
	options   *AddValidatorOptions
}

type removeValidatorParams struct {
	validator string
	options   *RemoveValidatorOptions
}

type reconcileStakingPoolsParams struct{}

type BlockHook struct {
	Block  int64       `yaml:"block"`
	Action hookType    `yaml:"action"`
	Params interface{} `yaml:"params"`
}

type BlockHooksConfig struct {
	Hooks []BlockHook `yaml:"block_hooks"`
}

// BlockHooks defines a map from block height to a list of hooks.
type BlockHooks map[int64][]BlockHook

// LoadHooks loads block hooks from the config file and panics in case
// any validation fails.
func (app *App) LoadHooks() BlockHooks {
	hooks := make(BlockHooks)
	data, err := lib.ReadConfigFile()

	if err != nil {
		return hooks
	}

	var config BlockHooksConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		panic(fmt.Errorf("failed to decode config %w", err))
	}

	for _, hook := range config.Hooks {
		var params interface{}

		block := hook.Block
		action := hook.Action

		switch action {
		case hookUnjail:
			params = app.parseUnjailHook(hook)
		case hookAddValidator:
			params = app.parseAddValidatorHook(hook)
		case hookRemoveValidator:
			params = app.parseRemoveValidatorHook(hook)
		case hookReconcileStakingPools:
			params = reconcileStakingPoolsParams{}
		default:
			panic(fmt.Sprintf("unexpected hook action %v", action))
		}

		hook.Params = params
		hooks[block] = append(hooks[block], hook)
	}

	return hooks
}

// parseUnjailHook validates and returns the parameters for a
// scheduled unjailing.
func (app *App) parseUnjailHook(hook BlockHook) unjailParams {
	params := expectParamsIsMap(hook.Params, "unjail")

	p := unjailParams{
		validator: requireFieldIsString(params, "validator"),
	}

	// Optional: target_tokens — after unjailing, mint and self-delegate until the
	// validator holds at least this many tokens. Use this to restore voting power
	// when a liveness slash has reduced tokens below the PowerReduction threshold.
	// If the validator already holds >= target_tokens, no minting occurs.
	if raw, ok := params["target_tokens"]; ok {
		p.targetTokens = parseInt(raw, "target_tokens")
		if !p.targetTokens.IsPositive() {
			panic(fmt.Sprintf("target_tokens must be positive, got %s", p.targetTokens.String()))
		}
	}

	return p
}

// parseAddValidatorHook validates and returns parameters for an add-validator hook.
func (app *App) parseAddValidatorHook(hook BlockHook) addValidatorParams {
	paramsMap := expectParamsIsMap(hook.Params, "add_validator")

	validatorStr := requireFieldIsString(paramsMap, "validator")

	pubKeyRaw, ok := paramsMap["pub_key"]
	if !ok {
		panic("failed to read pub_key field from params")
	}

	minSelfDelegationRaw, ok := paramsMap["min_self_delegation"]
	if !ok {
		panic("failed to read min_self_delegation field from params")
	}

	opts := &AddValidatorOptions{
		PubKey:            parsePubKey(pubKeyRaw),
		MinSelfDelegation: parseInt(minSelfDelegationRaw, "min_self_delegation"),
	}

	// Required: initial_tokens — amount to mint and self-delegate for this validator.
	initialTokensRaw, ok := paramsMap["initial_tokens"]
	if !ok {
		panic("failed to read initial_tokens field from params")
	}
	initialTokens := parseInt(initialTokensRaw, "initial_tokens")
	if !initialTokens.IsPositive() {
		panic(fmt.Sprintf("initial_tokens must be positive, got %s", initialTokens.String()))
	}
	opts.InitialTokens = initialTokens

	return addValidatorParams{
		validator: validatorStr,
		options:   opts,
	}
}

// parseRemoveValidatorHook validates and returns parameters for a remove-validator hook.
// Options:
//   - validator (required): validator operator address
//   - burn_tokens (optional, default false): if true, burn all validator tokens from bonded pool
//   - token_recipient (optional): address to send tokens to (ignored if burn_tokens is true)
//
// Note: When tokens are burned, all delegators lose their stake (similar to 100% slashing).
// Delegations are handled by the normal unbonding process after jailing.
func (app *App) parseRemoveValidatorHook(hook BlockHook) removeValidatorParams {
	paramsMap := expectParamsIsMap(hook.Params, "remove_validator")

	validatorStr := requireFieldIsString(paramsMap, "validator")

	opts := &RemoveValidatorOptions{}

	// Optional: burn_tokens (default false)
	if burnTokensRaw, ok := paramsMap["burn_tokens"]; ok {
		opts.BurnTokens = parseBool(burnTokensRaw, "burn_tokens")
	}

	// Optional: token_recipient (where to send tokens if not burning)
	if recipientRaw, ok := paramsMap["token_recipient"]; ok {
		recipientStr := requireValueIsString(recipientRaw, "token_recipient")
		recipientAddr, err := sdk.AccAddressFromBech32(recipientStr)
		if err != nil {
			panic(fmt.Sprintf("invalid token_recipient address: %v", err))
		}
		opts.TokenRecipient = recipientAddr
	}

	return removeValidatorParams{
		validator: validatorStr,
		options:   opts,
	}
}

// PerformHook performs a particular Cosmos hook.
func (app *App) PerformHook(ctx sdk.Context, hook BlockHook) error {
	switch hook.Action {
	case hookUnjail:
		return app.performUnjailHook(ctx, hook)
	case hookAddValidator:
		return app.performAddValidatorHook(ctx, hook)
	case hookRemoveValidator:
		return app.performRemoveValidatorHook(ctx, hook)
	case hookReconcileStakingPools:
		return app.performReconcileStakingPoolsHook(ctx, hook)
	default:
		return fmt.Errorf("unrecognized hook action %v", hook.Action)
	}
}

// performUnjailHook performs an unjailing.
func (app *App) performUnjailHook(ctx sdk.Context, hook BlockHook) error {
	params, ok := hook.Params.(unjailParams)
	if !ok {
		return fmt.Errorf("invalid unjail params %v", params)
	}

	valAddr, err := sdk.ValAddressFromBech32(params.validator)
	if err != nil {
		return err
	}

	var targetTokens *sdkmath.Int
	if !params.targetTokens.IsNil() && params.targetTokens.IsPositive() {
		targetTokens = &params.targetTokens
	}

	return app.ExecuteUnjailing(ctx, valAddr, targetTokens)
}

// performAddValidatorHook performs an add-validator hook.
func (app *App) performAddValidatorHook(ctx sdk.Context, hook BlockHook) error {
	params, ok := hook.Params.(addValidatorParams)
	if !ok {
		return fmt.Errorf("invalid add_validator params %v", hook.Params)
	}

	valAddr, err := sdk.ValAddressFromBech32(params.validator)
	if err != nil {
		return err
	}

	return app.ExecuteAddValidator(ctx, valAddr, params.options)
}

// performRemoveValidatorHook performs a remove-validator hook.
func (app *App) performRemoveValidatorHook(ctx sdk.Context, hook BlockHook) error {
	params, ok := hook.Params.(removeValidatorParams)
	if !ok {
		return fmt.Errorf("invalid remove_validator params %v", hook.Params)
	}

	valAddr, err := sdk.ValAddressFromBech32(params.validator)
	if err != nil {
		return err
	}

	return app.ExecuteRemoveValidator(ctx, valAddr, params.options)
}

// performReconcileStakingPoolsHook mints any tokens missing from the bonded or
// not-bonded pool so that pool balances match the sum of validator tokens.
func (app *App) performReconcileStakingPoolsHook(ctx sdk.Context, hook BlockHook) error {
	if _, ok := hook.Params.(reconcileStakingPoolsParams); !ok {
		return fmt.Errorf("invalid reconcile_staking_pools params %v", hook.Params)
	}
	app.ExecuteReconcileStakingPools(ctx)
	return nil
}

func expectParamsIsMap(raw interface{}, action string) map[string]interface{} {
	params, ok := raw.(map[string]interface{})
	if !ok {
		panic(fmt.Sprintf("failed to read %s params as map", action))
	}
	return params
}

func requireFieldIsString(params map[string]interface{}, key string) string {
	value, exists := params[key]
	if !exists {
		panic(fmt.Sprintf("failed to read %s field from params", key))
	}

	return requireValueIsString(value, key)
}

func requireValueIsString(value interface{}, field string) string {
	strValue, ok := value.(string)
	if !ok {
		panic(fmt.Sprintf("%s field is not type string", field))
	}

	return strValue
}

func parseInt(value interface{}, field string) sdkmath.Int {
	strValue := requireValueIsString(value, field)

	intValue, err := strconv.ParseInt(strValue, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("failed to parse %s as int64: %v", field, err))
	}

	return sdkmath.NewInt(intValue)
}

func parseBool(value interface{}, field string) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		boolValue, err := strconv.ParseBool(v)
		if err != nil {
			panic(fmt.Sprintf("failed to parse %s as bool: %v", field, err))
		}
		return boolValue
	default:
		panic(fmt.Sprintf("%s field is not a bool or string", field))
	}
}

func parsePubKey(value interface{}) cryptotypes.PubKey {
	pubKeyMap := expectParamsIsMap(value, "pub_key")

	keyType := strings.ToLower(requireFieldIsString(pubKeyMap, "type"))
	if keyType == "" {
		keyType = ed25519.KeyType
	}

	rawValue := requireFieldIsString(pubKeyMap, "value")
	decoded, err := base64.StdEncoding.DecodeString(rawValue)
	if err != nil {
		panic(fmt.Sprintf("failed to decode pub_key value: %v", err))
	}

	switch keyType {
	case strings.ToLower(ed25519.KeyType), "tendermint/pubkeyed25519":
		if len(decoded) != ed25519.PubKeySize {
			panic(fmt.Sprintf("invalid ed25519 pubkey size: %d", len(decoded)))
		}
		return &ed25519.PubKey{Key: decoded}
	default:
		panic(fmt.Sprintf("unsupported pub_key type %q", keyType))
	}
}

// hookLogger defines a custom logger for "hooks".
func (app *App) hookLogger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "hook")
}
