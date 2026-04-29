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
