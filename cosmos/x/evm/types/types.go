package types

import (
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

type TestFieldValue string

// BlockState represents the current block state information
type BlockState struct {
	Hash      common.Hash `json:"hash"`
	Height    uint64      `json:"height"`
	Timestamp uint64      `json:"timestamp"`
}

// NewBlockState creates a new BlockState instance
func NewBlockState(hash common.Hash, height uint64, timestamp uint64) BlockState {
	return BlockState{
		Hash:      hash,
		Height:    height,
		Timestamp: timestamp,
	}
}

// MarshalJSON implements json.Marshaler
func (bs BlockState) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Hash      string `json:"hash"`
		Height    uint64 `json:"height"`
		Timestamp uint64 `json:"timestamp"`
	}{
		Hash:      bs.Hash.Hex(),
		Height:    bs.Height,
		Timestamp: bs.Timestamp,
	})
}

// UnmarshalJSON implements json.Unmarshaler
func (bs *BlockState) UnmarshalJSON(data []byte) error {
	var temp struct {
		Hash      string `json:"hash"`
		Height    uint64 `json:"height"`
		Timestamp uint64 `json:"timestamp"`
	}
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}
	bs.Hash = common.HexToHash(temp.Hash)
	bs.Height = temp.Height
	bs.Timestamp = temp.Timestamp
	return nil
}

// Marshal encodes BlockState to bytes
func (bs BlockState) Marshal() ([]byte, error) {
	return json.Marshal(bs)
}

// Unmarshal decodes bytes to BlockState
func (bs *BlockState) Unmarshal(data []byte) error {
	return json.Unmarshal(data, bs)
}

// BlockStateValue implements collections.ValueCodec for BlockState
type BlockStateValue struct{}

// Encode implements collections.ValueCodec
func (BlockStateValue) Encode(value BlockState) ([]byte, error) {
	return value.Marshal()
}

// Decode implements collections.ValueCodec
func (BlockStateValue) Decode(b []byte) (BlockState, error) {
	var bs BlockState
	err := bs.Unmarshal(b)
	return bs, err
}

// EncodeJSON implements collections.ValueCodec
func (BlockStateValue) EncodeJSON(value BlockState) ([]byte, error) {
	return json.Marshal(value)
}

// DecodeJSON implements collections.ValueCodec
func (BlockStateValue) DecodeJSON(b []byte) (BlockState, error) {
	var bs BlockState
	err := json.Unmarshal(b, &bs)
	return bs, err
}

// Stringify implements collections.ValueCodec
func (BlockStateValue) Stringify(value BlockState) string {
	return fmt.Sprintf("%s:%d:%d", value.Hash.Hex(), value.Height, value.Timestamp)
}

// ValueType implements collections.ValueCodec
func (BlockStateValue) ValueType() string {
	return "nexus/x/evm/types.BlockState"
}
