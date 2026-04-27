package server

import (
	"fmt"
	"net"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetTestEngineUrl reserves a free localhost port and returns the http URL.
func GetTestEngineUrl() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return "", fmt.Errorf("unexpected addr type %T", listener.Addr())
	}

	return fmt.Sprintf("http://127.0.0.1:%d", tcpAddr.Port), nil
}

// AccAddress returns a server account address
func AccAddress() string {
	pk := ed25519.GenPrivKey().PubKey()
	addr := pk.Address()
	return sdk.AccAddress(addr).String()
}
