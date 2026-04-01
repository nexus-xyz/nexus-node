package app

import (
	"fmt"
	"time"

	"cosmossdk.io/math"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

const (
	// All validators must have exactly this many tokens when active.
	RequiredValidatorBalance = 1000000
)

// ensureValidatorBalance checks that a bonded validator has exactly
// RequiredValidatorBalance tokens. Returns an error if the invariant is violated,
// preventing the chain from starting with misconfigured validators that would cause
// bonded pool accounting mismatches and eventual consensus failure on removal.
func ensureValidatorBalance(validator stakingtypes.Validator) error {
	if validator.Status != stakingtypes.Bonded {
		return nil
	}
	required := math.NewInt(RequiredValidatorBalance)
	if !validator.Tokens.Equal(required) {
		return fmt.Errorf(
			"genesis validator %s has %s bonded tokens, must be exactly %d",
			validator.OperatorAddress, validator.Tokens.String(), RequiredValidatorBalance,
		)
	}
	return nil
}

// AddValidatorOptions controls how ExecuteAddValidator initializes validator state when missing.
type AddValidatorOptions struct {
	PubKey            cryptotypes.PubKey
	Description       stakingtypes.Description
	InitialTokens     math.Int // Starting delegation amount; setValidatorBalance tops up to RequiredValidatorBalance.
	MinSelfDelegation math.Int
}

// RemoveValidatorOptions controls how ExecuteRemoveValidator handles cleanup.
type RemoveValidatorOptions struct {
	BurnTokens     bool           // If true, burn validator's bonded tokens from the bonded pool
	TokenRecipient sdk.AccAddress // Where to send tokens if not burning (ignored if BurnTokens=true)
}

// Set validator balance to required amount.
func (app *App) setValidatorBalance(ctx sdk.Context, valAddr sdk.ValAddress, validator stakingtypes.Validator) error {
	tokens := validator.Tokens
	if tokens.IsNil() {
		tokens = math.ZeroInt()
	}

	if tokens.LT(math.NewInt(RequiredValidatorBalance)) {
		bondDenom := sdk.DefaultBondDenom
		tokensToRestore := math.NewInt(RequiredValidatorBalance).Sub(tokens)
		additionalTokens := sdk.NewCoin(bondDenom, tokensToRestore)

		// Mint the additional tokens to the staking bonded pool (which has Minter permissions)
		if err := app.BankKeeper.MintCoins(
			ctx,
			stakingtypes.BondedPoolName,
			sdk.NewCoins(additionalTokens),
		); err != nil {
			return fmt.Errorf("failed to mint additional tokens: %w", err)
		}

		// Send tokens from staking bonded pool to validator account
		delegatorAddress := sdk.AccAddress(valAddr.Bytes())
		if err := app.BankKeeper.SendCoinsFromModuleToAccount(
			ctx,
			stakingtypes.BondedPoolName,
			delegatorAddress,
			sdk.NewCoins(additionalTokens),
		); err != nil {
			return fmt.Errorf("failed to send minted tokens to validator account: %w", err)
		}

		// Delegate the tokens to the validator
		delegateMsg := stakingtypes.MsgDelegate{
			DelegatorAddress: delegatorAddress.String(),
			ValidatorAddress: valAddr.String(),
			Amount:           additionalTokens,
		}
		if _, err := stakingkeeper.NewMsgServerImpl(app.StakingKeeper).Delegate(ctx, &delegateMsg); err != nil {
			return fmt.Errorf("failed to delegate tokens to validator account: %w", err)
		}
	}

	return nil
}

// ExecuteUnjailing performs the unjailing process for a validator, recovering slashed tokens
func (app *App) ExecuteUnjailing(ctx sdk.Context, valAddr sdk.ValAddress) error {
	ctx.Logger().Info("[UNJAIL] Starting unjailing process", "validator", valAddr.String())

	// Get the validator to check current staked tokens
	validator, err := app.StakingKeeper.GetValidator(ctx, valAddr)
	if err != nil {
		return fmt.Errorf("failed to get validator: %w", err)
	}

	ctx.Logger().Info("[UNJAIL] Validator status",
		"jailed", validator.Jailed,
		"tokens", validator.Tokens.String(),
		"status", validator.Status.String())

	// Calculate the expected tokens (minimum required balance)
	requiredTokens := math.NewInt(RequiredValidatorBalance)
	currentTokens := validator.Tokens

	ctx.Logger().Info("[UNJAIL] Token comparison",
		"required", requiredTokens.String(),
		"current", currentTokens.String(),
		"needsRestore", currentTokens.LT(requiredTokens))

	if err := app.setValidatorBalance(ctx, valAddr, validator); err != nil {
		return fmt.Errorf("failed to ensure validator balance: %w", err)
	}

	// Get the consensus address for the validator
	consAddr, err := validator.GetConsAddr()
	if err != nil {
		ctx.Logger().Error("[UNJAIL] Failed to get consensus address", "error", err)
		return fmt.Errorf("failed to get consensus address: %w", err)
	}

	// Get validator signing info to check jail status
	signingInfo, err := app.SlashingKeeper.GetValidatorSigningInfo(ctx, consAddr)
	if err != nil {
		ctx.Logger().Error("[UNJAIL] Failed to get signing info", "error", err)
		return fmt.Errorf("failed to get signing info: %w", err)
	}

	ctx.Logger().Info("[UNJAIL] Signing info",
		"jailedUntil", signingInfo.JailedUntil.String(),
		"currentTime", ctx.BlockTime().String(),
		"canUnjail", ctx.BlockTime().After(signingInfo.JailedUntil))

	// Clear the jailed until time to allow immediate unjailing
	signingInfo.JailedUntil = ctx.BlockTime().Add(-1 * time.Second)
	if err := app.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr, signingInfo); err != nil {
		ctx.Logger().Error("[UNJAIL] Failed to update signing info", "error", err)
		return fmt.Errorf("failed to update signing info: %w", err)
	}

	ctx.Logger().Info("[UNJAIL] Updated signing info to allow unjailing")

	// Unjail the validator
	ctx.Logger().Info("[UNJAIL] Attempting to unjail validator")
	if err := app.SlashingKeeper.Unjail(ctx, valAddr); err != nil {
		ctx.Logger().Error("[UNJAIL] Failed to unjail", "error", err)
		return fmt.Errorf("failed to unjail validator: %w", err)
	}

	ctx.Logger().Info("[UNJAIL] Validator unjailed successfully")
	return nil
}

// ExecuteAddValidator ensures a validator exists in the active set by creating
// a minimal validator record when missing, topping up self-delegation (if needed),
// and unjailing when applicable.
func (app *App) ExecuteAddValidator(ctx sdk.Context, valAddr sdk.ValAddress, opts *AddValidatorOptions) error {
	ctx.Logger().Info("[ADDVAL] Starting add-validator flow", "validator", valAddr.String())

	if opts != nil && !opts.InitialTokens.IsNil() && !opts.InitialTokens.Equal(math.NewInt(RequiredValidatorBalance)) {
		return fmt.Errorf(
			"initial_tokens must equal RequiredValidatorBalance (%d), got %s",
			RequiredValidatorBalance,
			opts.InitialTokens.String(),
		)
	}

	validator, err := app.StakingKeeper.GetValidator(ctx, valAddr)
	if err != nil {
		if opts == nil || opts.PubKey == nil {
			return fmt.Errorf("validator not found: %w (provide AddValidatorOptions with PubKey to create)", err)
		}

		delegatorAddr := sdk.AccAddress(valAddr.Bytes())
		ctx.Logger().Info("[ADDVAL] Checking if delegator account exists", "delegator", delegatorAddr.String())
		if !app.AuthKeeper.HasAccount(ctx, delegatorAddr) {
			newAccount := app.AuthKeeper.NewAccountWithAddress(ctx, delegatorAddr)
			app.AuthKeeper.SetAccount(ctx, newAccount)
		}

		description := opts.Description
		validator, err = stakingtypes.NewValidator(valAddr.String(), opts.PubKey, description)
		if err != nil {
			return fmt.Errorf("failed to build validator: %w", err)
		}

		// Always start with zero tokens and let setValidatorBalance fund the full
		// RequiredValidatorBalance via real minting and delegation. Setting Tokens
		// directly (e.g. opts.InitialTokens) created phantom tokens: the validator
		// record claimed a balance that was never deposited into the bonded pool,
		// which caused a consensus failure panic on removal with burn_tokens.
		// InitialTokens is accepted for config compatibility; setValidatorBalance
		// enforces the final balance is exactly RequiredValidatorBalance.
		validator.Status = stakingtypes.Bonded
		validator.Tokens = math.ZeroInt()
		validator.DelegatorShares = math.LegacyZeroDec()

		ctx.Logger().Info(
			"[ADDVAL] Validator min self delegation",
			"minSelfDelegation",
			validator.MinSelfDelegation.String(),
		)

		if !opts.MinSelfDelegation.IsNil() && opts.MinSelfDelegation.IsPositive() {
			validator.MinSelfDelegation = opts.MinSelfDelegation

			if err := app.StakingKeeper.SetValidator(ctx, validator); err != nil {
				return fmt.Errorf("failed to persist validator: %w", err)
			}
			if err := app.StakingKeeper.SetValidatorByConsAddr(ctx, validator); err != nil {
				return fmt.Errorf("failed to index validator by cons addr: %w", err)
			}
			if err := app.StakingKeeper.SetNewValidatorByPowerIndex(ctx, validator); err != nil {
				return fmt.Errorf("failed to index validator by power: %w", err)
			}

			// Initialize signing info for the validator
			// We still need to have a validator up and running on docker to pick up this job
			consAddr, err := validator.GetConsAddr()
			if err != nil {
				return fmt.Errorf("failed to get consensus address: %w", err)
			}

			_, err = app.SlashingKeeper.GetValidatorSigningInfo(ctx, consAddr)
			if err != nil {
				// Signing info doesn't exist, create it
				ctx.Logger().Info("[ADDVAL] Initializing validator signing info", "validator", valAddr.String())
				signingInfo := slashingtypes.NewValidatorSigningInfo(
					consAddr,
					ctx.BlockHeight(),
					ctx.BlockHeight()-1,
					time.Unix(0, 0),
					false, // not tombstoned
					0,     // missed blocks counter
				)
				if err := app.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr, signingInfo); err != nil {
					return fmt.Errorf("failed to set signing info: %w", err)
				}
			}

			if err := app.DistrKeeper.SetValidatorHistoricalRewards(
				ctx,
				valAddr,
				0,
				distrtypes.NewValidatorHistoricalRewards(sdk.DecCoins{}, 1),
			); err != nil {
				return fmt.Errorf("failed to set validator historical rewards: %w", err)
			}
			if err := app.DistrKeeper.SetValidatorCurrentRewards(
				ctx,
				valAddr,
				distrtypes.NewValidatorCurrentRewards(sdk.DecCoins{}, 1),
			); err != nil {
				return fmt.Errorf("failed to set validator current rewards: %w", err)
			}
			if err := app.DistrKeeper.SetValidatorAccumulatedCommission(
				ctx,
				valAddr,
				distrtypes.InitialValidatorAccumulatedCommission(),
			); err != nil {
				return fmt.Errorf("failed to set validator accumulated commission: %w", err)
			}

			if _, err := app.DistrKeeper.FeePool.Get(ctx); err != nil {
				if err := app.DistrKeeper.FeePool.Set(ctx, distrtypes.InitialFeePool()); err != nil {
					return fmt.Errorf("failed to initialize fee pool: %w", err)
				}
			}

			validator, err = app.StakingKeeper.GetValidator(ctx, valAddr)
			if err != nil {
				return fmt.Errorf("failed to load validator after creation: %w", err)
			}
		}

		if err := app.setValidatorBalance(ctx, valAddr, validator); err != nil {
			return fmt.Errorf("failed to ensure validator balance: %w", err)
		}

		ctx.Logger().Info("[ADDVAL] Add-validator flow complete")
	}

	return nil
}

// ExecuteRemoveValidator removes a validator from the active set by marking them as jailed.
// Optionally burns or transfers the validator's bonded tokens.
// Panics on any failure since this is a critical operation that must fully succeed.
func (app *App) ExecuteRemoveValidator(ctx sdk.Context, valAddr sdk.ValAddress, opts *RemoveValidatorOptions) error {
	ctx.Logger().Info("[RMVAL] Starting remove-validator flow", "validator", valAddr.String())

	// 1. Get the validator
	validator, err := app.StakingKeeper.GetValidator(ctx, valAddr)
	if err != nil {
		panic(fmt.Sprintf("[RMVAL] validator not found: %v", err))
	}

	ctx.Logger().Info("[RMVAL] Found validator",
		"status", validator.Status.String(),
		"tokens", validator.Tokens.String(),
		"jailed", validator.Jailed)

	// 2. Get consensus address for slashing operations
	consAddr, err := validator.GetConsAddr()
	if err != nil {
		panic(fmt.Sprintf("[RMVAL] failed to get consensus address: %v", err))
	}

	// 3. Tombstone the validator to permanently prevent unjailing
	app.tombstoneValidator(ctx, consAddr)

	// 4. Remove from power index FIRST (uses current tokens for key lookup)
	// This MUST succeed before setting Jailed=true, otherwise the staking module will panic
	// with "should never retrieve a jailed validator from the power store"
	if !validator.Jailed {
		ctx.Logger().Info("[RMVAL] Removing validator from power index")
		if err := app.StakingKeeper.DeleteValidatorByPowerIndex(ctx, validator); err != nil {
			panic(fmt.Sprintf("[RMVAL] failed to delete validator from power index: %v", err))
		}
	}

	// 5. Handle token burning/transfer (while validator is still bonded)
	tokensBurned := app.handleValidatorTokens(ctx, validator, opts)

	// 6. Update validator record - set jailed and zero tokens if burned
	if !validator.Jailed || tokensBurned {
		ctx.Logger().Info("[RMVAL] Updating validator record")
		validator.Jailed = true
		if tokensBurned {
			validator.Tokens = math.ZeroInt()
			validator.DelegatorShares = math.LegacyZeroDec()
		}
		if err := app.StakingKeeper.SetValidator(ctx, validator); err != nil {
			panic(fmt.Sprintf("[RMVAL] failed to update validator: %v", err))
		}
	}

	ctx.Logger().Info("[RMVAL] Remove-validator flow complete - validator marked as jailed and tombstoned",
		"validator", valAddr.String())
	return nil
}

// tombstoneValidator marks a validator as tombstoned to permanently prevent unjailing.
func (app *App) tombstoneValidator(ctx sdk.Context, consAddr sdk.ConsAddress) {
	signingInfo, err := app.SlashingKeeper.GetValidatorSigningInfo(ctx, consAddr)
	if err != nil {
		panic(fmt.Sprintf("[RMVAL] failed to get validator signing info: %v", err))
	}

	if signingInfo.Tombstoned {
		ctx.Logger().Info("[RMVAL] Validator already tombstoned")
		return
	}

	ctx.Logger().Info("[RMVAL] Tombstoning validator")
	signingInfo.Tombstoned = true
	signingInfo.JailedUntil = time.Unix(253402300799, 0) // Year 9999
	if err := app.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr, signingInfo); err != nil {
		panic(fmt.Sprintf("[RMVAL] failed to tombstone validator: %v", err))
	}
}

// handleValidatorTokens burns or transfers validator tokens based on options.
// Returns true if tokens were burned/transferred and validator record needs updating.
func (app *App) handleValidatorTokens(
	ctx sdk.Context,
	validator stakingtypes.Validator,
	opts *RemoveValidatorOptions,
) bool {
	if opts == nil || !validator.Tokens.IsPositive() {
		return false
	}
	if !opts.BurnTokens && opts.TokenRecipient == nil {
		return false
	}

	bondDenom, err := app.StakingKeeper.BondDenom(ctx)
	if err != nil {
		panic(fmt.Sprintf("[RMVAL] failed to get bond denom: %v", err))
	}

	// Unbonded/unbonding validators have their tokens in the not-bonded pool;
	// bonded validators have theirs in the bonded pool.
	poolName := stakingtypes.BondedPoolName
	if validator.Status != stakingtypes.Bonded {
		poolName = stakingtypes.NotBondedPoolName
	}

	// Cap to the actual pool balance to handle state mismatches where validator.Tokens
	// exceeds what is held in the pool (e.g. phantom tokens written via SetValidator
	// without a corresponding bank deposit when initial_tokens >= RequiredValidatorBalance).
	poolAddr := app.AuthKeeper.GetModuleAddress(poolName)
	poolBalance := app.BankKeeper.GetBalance(ctx, poolAddr, bondDenom)
	amount := validator.Tokens
	if poolBalance.Amount.LT(amount) {
		ctx.Logger().Warn("[RMVAL] Pool balance less than validator tokens; capping",
			"pool", poolName,
			"poolBalance", poolBalance.Amount.String(),
			"validatorTokens", amount.String())
		amount = poolBalance.Amount
	}
	if !amount.IsPositive() {
		return false
	}
	coins := sdk.NewCoins(sdk.NewCoin(bondDenom, amount))

	if opts.BurnTokens {
		ctx.Logger().Info("[RMVAL] Burning validator tokens", "pool", poolName, "amount", coins.String())
		if err := app.BankKeeper.BurnCoins(ctx, poolName, coins); err != nil {
			panic(fmt.Sprintf("[RMVAL] failed to burn tokens from %s: %v", poolName, err))
		}
		ctx.Logger().Info("[RMVAL] Successfully burned tokens", "amount", coins.String())
		return true
	}

	if opts.TokenRecipient != nil {
		ctx.Logger().Info("[RMVAL] Transferring validator tokens",
			"pool", poolName, "amount", coins.String(), "to", opts.TokenRecipient.String())
		if err := app.BankKeeper.SendCoinsFromModuleToAccount(
			ctx, poolName, opts.TokenRecipient, coins,
		); err != nil {
			panic(fmt.Sprintf("[RMVAL] failed to transfer tokens from %s: %v", poolName, err))
		}
		ctx.Logger().Info("[RMVAL] Successfully transferred tokens", "amount", coins.String())
		return true
	}

	return false
}

// ExecuteReconcileStakingPools mints any tokens missing from the bonded or not-bonded
// pool so that pool balances match the sum of validator tokens plus in-flight
// unbonding delegation balances.
//
// This corrects state corruption introduced by the old add_validator hook code
// that wrote phantom validator.Tokens directly via SetValidator without depositing
// matching coins into the bonded pool.  When those validators were jailed the
// staking module moved real coins from the bonded pool (funded by genesis
// validators) into the not-bonded pool, gradually depleting the bonded pool until
// it could no longer cover the next slashing event.
//
// Reconciliation runs before ModuleManager.EndBlock so the staking module never
// sees a deficit.  It is idempotent: when the pools are correctly funded no coins
// are minted.
func (app *App) ExecuteReconcileStakingPools(ctx sdk.Context) {
	logger := app.hookLogger(ctx)
	bondDenom := sdk.DefaultBondDenom

	validators, err := app.StakingKeeper.GetAllValidators(ctx)
	if err != nil {
		logger.Error("reconcileStakingPools: failed to get validators", "error", err)
		return
	}

	bondedSum := math.ZeroInt()
	notBondedSum := math.ZeroInt()
	for _, v := range validators {
		if v.Tokens.IsNil() {
			continue
		}
		if v.IsBonded() {
			bondedSum = bondedSum.Add(v.Tokens)
		} else {
			notBondedSum = notBondedSum.Add(v.Tokens)
		}
	}

	// Unbonding delegations are held in the not-bonded pool but are already
	// subtracted from validator.Tokens, so we must count them separately.
	if err := app.StakingKeeper.IterateUnbondingDelegations(
		ctx,
		func(_ int64, ubd stakingtypes.UnbondingDelegation) bool {
			for _, entry := range ubd.Entries {
				if !entry.Balance.IsNil() && entry.Balance.IsPositive() {
					notBondedSum = notBondedSum.Add(entry.Balance)
				}
			}
			return false
		}); err != nil {
		logger.Error("reconcileStakingPools: failed to iterate UBDs", "error", err)
	}

	ctx.Logger().Info("[RecStakingPools] Bonded pool balance", "amount", bondedSum.String())
	ctx.Logger().Info("[RecStakingPools] Not bonded pool balance", "amount", notBondedSum.String())

	bondedPoolAddr := app.AuthKeeper.GetModuleAddress(stakingtypes.BondedPoolName)
	bondedBal := app.BankKeeper.GetBalance(ctx, bondedPoolAddr, bondDenom).Amount

	ctx.Logger().Info("[RecStakingPools] Bonded pool balance", "amount", bondedBal.String())

	if bondedBal.LT(bondedSum) {
		deficit := bondedSum.Sub(bondedBal)
		logger.Warn("BondedPool underfunded; minting deficit", "deficit", deficit.String())
		coins := sdk.NewCoins(sdk.NewCoin(bondDenom, deficit))
		if err := app.BankKeeper.MintCoins(ctx, stakingtypes.BondedPoolName, coins); err != nil {
			logger.Error("reconcileStakingPools: failed to mint BondedPool deficit", "error", err)
		}
		ctx.Logger().Info("[RecStakingPools] Minted BondedPool deficit", "amount", deficit.String())
	}

	notBondedPoolAddr := app.AuthKeeper.GetModuleAddress(stakingtypes.NotBondedPoolName)
	notBondedBal := app.BankKeeper.GetBalance(ctx, notBondedPoolAddr, bondDenom).Amount

	ctx.Logger().Info("[RecStakingPools] Not bonded pool balance", "amount", notBondedBal.String())

	if notBondedBal.LT(notBondedSum) {
		deficit := notBondedSum.Sub(notBondedBal)
		logger.Warn("NotBondedPool underfunded; minting deficit", "deficit", deficit.String())
		coins := sdk.NewCoins(sdk.NewCoin(bondDenom, deficit))
		// NotBondedPool lacks Minter permission; mint to BondedPool then transfer.
		if err := app.BankKeeper.MintCoins(ctx, stakingtypes.BondedPoolName, coins); err != nil {
			logger.Error("reconcileStakingPools: failed to mint for NotBondedPool deficit", "error", err)
			return
		}
		ctx.Logger().Info("[RecStakingPools] Minted BondedPool not bonded pool deficit", "amount", deficit.String())
		if err := app.BankKeeper.SendCoinsFromModuleToModule(
			ctx,
			stakingtypes.BondedPoolName,
			stakingtypes.NotBondedPoolName,
			coins,
		); err != nil {
			logger.Error("reconcileStakingPools: failed to transfer to NotBondedPool", "error", err)
		}
		ctx.Logger().Info(
			"[RecStakingPools] Deficit transferred from BondedPool to NotBondedPool", "amount", deficit.String(),
		)
	}
}
