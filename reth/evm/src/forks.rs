use std::sync::Arc;

use reth_chainspec::{ChainSpec, ForkCondition, hardfork};
use reth_cli::chainspec::{ChainSpecParser, parse_genesis};
use serde::{Deserialize, Serialize};

hardfork!(Nexus { V0 });

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct NexusHardforkConfig {
    #[serde(default)]
    pub v0_time: Option<u64>,
}

#[derive(Debug, Clone, Default)]
pub struct NexusChainSpecParser;

impl ChainSpecParser for NexusChainSpecParser {
    type ChainSpec = ChainSpec;

    const SUPPORTED_CHAINS: &'static [&'static str] = &[];

    fn parse(s: &str) -> eyre::Result<Arc<Self::ChainSpec>> {
        let genesis = parse_genesis(s)?;
        let extra_fields = genesis
            .config
            .extra_fields
            .deserialize_as::<NexusHardforkConfig>()?;

        // Insert hard-forks (if specified)
        let mut chain_spec = ChainSpec::from_genesis(genesis);

        if let Some(v0_time) = extra_fields.v0_time {
            chain_spec
                .hardforks
                .insert(Nexus::V0, ForkCondition::Timestamp(v0_time));
        }

        Ok(Arc::new(chain_spec))
    }
}
