package types

import "github.com/ethereum/go-ethereum/common/hexutil"

func (d *DepositRequest) Encode() []hexutil.Bytes {
	panic("not implemented")
}

func (w *WithdrawalRequest) Encode() []hexutil.Bytes {
	panic("not implemented")
}

func (c *ConsensusRequests) Encode() []hexutil.Bytes {
	panic("not implemented")
}
