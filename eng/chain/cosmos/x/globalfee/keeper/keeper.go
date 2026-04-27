package keeper

import (
	"context"
	"encoding/json"

	corestore "cosmossdk.io/core/store"

	"nexus/x/globalfee/types"
)

type Keeper struct {
	storeService corestore.KVStoreService
}

func NewKeeper(storeService corestore.KVStoreService) Keeper {
	return Keeper{storeService: storeService}
}

func (k Keeper) GetParams(ctx context.Context) types.Params {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get(types.ParamsKey)
	if err != nil {
		panic(err)
	}
	if bz == nil {
		return types.DefaultParams()
	}
	var p types.Params
	if err := json.Unmarshal(bz, &p); err != nil {
		panic(err)
	}
	return p
}

func (k Keeper) SetParams(ctx context.Context, p types.Params) error {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return store.Set(types.ParamsKey, bz)
}
