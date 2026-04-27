package keeper

import "fmt"

func toUint64(val int64) uint64 {
	if val < 0 {
		panic(fmt.Errorf("expected non-negative value, got: %v", val))
	}

	return uint64(val)
}
