package genesis_test

import (
	"encoding/json"
	"testing"

	"nexus/genesis"
)

func TestGenesis_AllEmbeddedNetworksAreValidJSON(t *testing.T) {
	for _, name := range genesis.Names() {
		t.Run(name, func(t *testing.T) {
			data, err := genesis.Genesis(name)
			if err != nil {
				t.Fatalf("Genesis(%q): %v", name, err)
			}
			if len(data) == 0 {
				t.Fatalf("Genesis(%q): empty", name)
			}
			var any map[string]any
			if err := json.Unmarshal(data, &any); err != nil {
				t.Fatalf("Genesis(%q): not valid JSON: %v", name, err)
			}
			if _, ok := any["chain_id"]; !ok {
				t.Fatalf("Genesis(%q): missing chain_id", name)
			}
		})
	}
}

// TestGenesis_KnownValidatorAddresses checks that each embedded genesis
// contains the expected set of genesis validator operator addresses.
// Update these lists whenever genesis files change.
func TestGenesis_KnownValidatorAddresses(t *testing.T) {
	type genTxMsg struct {
		Type             string `json:"@type"`
		ValidatorAddress string `json:"validator_address"`
	}
	type genTxBody struct {
		Messages []genTxMsg `json:"messages"`
	}
	type genTx struct {
		Body genTxBody `json:"body"`
	}
	type genUtil struct {
		GenTxs []genTx `json:"gen_txs"`
	}
	type appState struct {
		GenUtil genUtil `json:"genutil"`
	}
	type genesisDoc struct {
		AppState appState `json:"app_state"`
	}

	tests := []struct {
		network string
		addrs   []string
	}{
		{
			network: "localnet",
			addrs: []string{
				"nexusvaloper1m6jc29uar9n07mr0srspc7skjs7qvjkvw3h5kf",
				"nexusvaloper17ze54jawc4etntrlz997wtkt8w3gwndvmehgex",
				"nexusvaloper1pc7etdjdu525mspvunghh6pl0fr554466ptj3w",
				"nexusvaloper1nv4jg4xd3rqx6r3z9esyt2pll96n0pu3mqthu3",
			},
		},
		{
			network: "devnet",
			addrs: []string{
				"nexusvaloper1skskq94z6dksapu8eh3htdvus2axtjdghwgmlr",
				"nexusvaloper1z8vwt5m7dxyg9wnpaw75lellehk6xqkrza94mw",
				"nexusvaloper14dd7t76ursgn0g5gl03uyd9rm5294tnv0zehhd",
				"nexusvaloper1cx5yuekmwyqrwsyc8s2u97r5lm9u8gauaaewp7",
			},
		},
		{
			network: "testnet",
			addrs: []string{
				"nexusvaloper176r5kzaxquu7n2mdxdczdh4f20xek64kmwyttl",
				"nexusvaloper1crg9a7804culh9nucmr3dezyk8eyedwl4f2pj3",
				"nexusvaloper1rah8shzznnr93v06xnl3tlhnvqnj4dgj3l3tgu",
				"nexusvaloper19s22yt9lxftmmxvxr56e30tdhcejm5ju75xl4g",
			},
		},
		{
			network: "mainnet",
			addrs: []string{
				"nexusvaloper1f55lp4c5my6fv8luxsg6ug5r8qtlefkpggvss5",
				"nexusvaloper1jpk5z2sxs259q3w0h3jjge5u89uhrym8mwexw0",
				"nexusvaloper1dtked3gkqu9tfspad2q2jmaup8syjpwwmkhyup",
				"nexusvaloper1uxnct7e6myya26n6r5xmuq48jvwr97lkpfyak7",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.network, func(t *testing.T) {
			data, err := genesis.Genesis(tt.network)
			if err != nil {
				t.Fatalf("Genesis(%q): %v", tt.network, err)
			}
			var doc genesisDoc
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatalf("Genesis(%q): unmarshal: %v", tt.network, err)
			}
			got := make(map[string]struct{})
			for _, tx := range doc.AppState.GenUtil.GenTxs {
				for _, msg := range tx.Body.Messages {
					if msg.ValidatorAddress != "" {
						got[msg.ValidatorAddress] = struct{}{}
					}
				}
			}
			for _, addr := range tt.addrs {
				if _, ok := got[addr]; !ok {
					t.Errorf("Genesis(%q): expected validator address %q not found in gen_txs", tt.network, addr)
				}
			}
			// Ensure the exact set — no extra validators silently added.
			want := make(map[string]struct{}, len(tt.addrs))
			for _, a := range tt.addrs {
				want[a] = struct{}{}
			}
			for addr := range got {
				if _, ok := want[addr]; !ok {
					t.Errorf("Genesis(%q): unexpected validator address %q in gen_txs", tt.network, addr)
				}
			}
		})
	}
}

func TestGenesis_UnknownNetworkErrors(t *testing.T) {
	if _, err := genesis.Genesis("does-not-exist"); err == nil {
		t.Fatal("Genesis(\"does-not-exist\") must return an error")
	}
}

func TestNames_Stable(t *testing.T) {
	got := genesis.Names()
	want := []string{"devnet", "localnet", "mainnet", "testnet"}
	if len(got) != len(want) {
		t.Fatalf("Names(): got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("Names()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
