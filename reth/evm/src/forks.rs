use std::sync::Arc;

use reth_chainspec::ChainSpec;
use reth_cli::chainspec::{ChainSpecParser, parse_genesis};

macro_rules! chain_spec {
    ($file:literal) => {
        include_str!(concat!("../../../cosmos/genesis/chain-specs/", $file))
    };
}

const LOCALNET_GENESIS: &str = chain_spec!("localnet/reth.json");
const DEVNET_GENESIS: &str = chain_spec!("devnet/reth.json");
const TESTNET_GENESIS: &str = chain_spec!("testnet/reth.json");
const MAINNET_GENESIS: &str = chain_spec!("mainnet/reth.json");

#[derive(Debug, Clone, Default)]
pub struct NexusChainSpecParser;

impl ChainSpecParser for NexusChainSpecParser {
    type ChainSpec = ChainSpec;

    const SUPPORTED_CHAINS: &'static [&'static str] = &["localnet", "devnet", "testnet", "mainnet"];

    fn parse(s: &str) -> eyre::Result<Arc<Self::ChainSpec>> {
        let genesis_str = match s {
            "localnet" => Some(LOCALNET_GENESIS),
            "devnet" => Some(DEVNET_GENESIS),
            "testnet" => Some(TESTNET_GENESIS),
            "mainnet" => Some(MAINNET_GENESIS),
            _ => None,
        };

        let genesis = if let Some(embedded) = genesis_str {
            serde_json::from_str(embedded)?
        } else {
            parse_genesis(s)?
        };

        let chain_spec = ChainSpec::from_genesis(genesis);
        Ok(Arc::new(chain_spec))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const MINIMAL_GENESIS_JSON: &str = r#"{
        "config": { "chainId": 392 },
        "alloc": {},
        "difficulty": "0x0",
        "gasLimit": "0x1c9c380"
    }"#;

    #[test]
    fn localnet_embedded_genesis_parses() {
        let spec = NexusChainSpecParser::parse("localnet").unwrap();
        assert_eq!(spec.chain.id(), 3941);
    }

    #[test]
    fn devnet_embedded_genesis_parses() {
        let spec = NexusChainSpecParser::parse("devnet").unwrap();
        assert_eq!(spec.chain.id(), 3940);
    }

    #[test]
    fn testnet_embedded_genesis_parses() {
        let spec = NexusChainSpecParser::parse("testnet").unwrap();
        assert_eq!(spec.chain.id(), 3945);
    }

    #[test]
    fn mainnet_embedded_genesis_parses() {
        let spec = NexusChainSpecParser::parse("mainnet").unwrap();
        assert_eq!(spec.chain.id(), 3946);
    }

    #[test]
    fn parse_accepts_inline_genesis_json() {
        let chain_spec = NexusChainSpecParser::parse(MINIMAL_GENESIS_JSON)
            .expect("minimal genesis json should parse");
        assert_eq!(chain_spec.chain.id(), 392);
    }

    #[test]
    fn parse_returns_error_for_unknown_chain() {
        // Input is neither a known embedded chain name nor a readable file path —
        // parse_genesis falls through to a filesystem read and returns an io::Error.
        let _ = NexusChainSpecParser::parse("not-json-and-not-a-path")
            .expect_err("garbage input must not parse as a chain spec");
    }
}
