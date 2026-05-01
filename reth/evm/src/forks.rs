use std::sync::Arc;

use reth_chainspec::ChainSpec;
use reth_cli::chainspec::{ChainSpecParser, parse_genesis};

#[derive(Debug, Clone, Default)]
pub struct NexusChainSpecParser;

impl ChainSpecParser for NexusChainSpecParser {
    type ChainSpec = ChainSpec;

    const SUPPORTED_CHAINS: &'static [&'static str] = &[];

    fn parse(s: &str) -> eyre::Result<Arc<Self::ChainSpec>> {
        let genesis = parse_genesis(s)?;
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
    fn parse_accepts_inline_genesis_json() {
        let chain_spec = NexusChainSpecParser::parse(MINIMAL_GENESIS_JSON)
            .expect("minimal genesis json should parse");
        assert_eq!(chain_spec.chain.id(), 392);
    }

    #[test]
    fn parse_returns_io_error_for_nonexistent_path() {
        // Valid relative path syntax but the file doesn't exist —
        // parse_genesis returns an io::Error.
        let _ = NexusChainSpecParser::parse("not-json-and-not-a-path")
            .expect_err("garbage input must not parse as a chain spec");
    }
}
