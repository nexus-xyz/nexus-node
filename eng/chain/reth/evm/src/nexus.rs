use std::sync::Arc;

use eyre::Context;
use reth_chainspec::{ChainSpec, ChainSpecBuilder, EthereumHardfork, ForkCondition};
use reth_tracing::tracing;
use serde::Deserialize;

use crate::forks::Nexus;

/// Configuration structure for Nexus Reth
#[derive(Debug, Clone, Deserialize)]
pub struct NexusConfig {
    #[serde(default)]
    pub fork_timings: ForkTimings,
}

#[derive(Debug, Clone, Deserialize, Default)]
pub struct ForkTimings {
    // ETH Forks
    #[serde(default)]
    pub prague_time: Option<u64>,

    // Nexus Forks
    #[serde(default)]
    pub v0_time: Option<u64>,
}

impl NexusConfig {
    pub fn apply_forks(&self, chain_spec: &Arc<ChainSpec>) -> ChainSpec {
        let mut builder = ChainSpecBuilder::from(chain_spec);

        if let Some(prague_time) = self.fork_timings.prague_time {
            builder = builder.without_fork(EthereumHardfork::Prague).with_fork(
                EthereumHardfork::Prague,
                ForkCondition::Timestamp(prague_time),
            );
            tracing::debug!("Applying Prague fork at timestamp {}", prague_time);
        }

        if let Some(v0_time) = self.fork_timings.v0_time {
            builder = builder
                .without_fork(Nexus::V0)
                .with_fork(Nexus::V0, ForkCondition::Timestamp(v0_time));
            tracing::debug!("Applying Nexus V0 fork at timestamp {}", v0_time);
        }

        builder.build()
    }

    /// Parse a YAML configuration file from a path
    pub fn parse_nexus_config(path: &str) -> eyre::Result<NexusConfig> {
        let file =
            std::fs::File::open(path).with_context(|| format!("Opening config file '{}'", path))?;

        let config: NexusConfig = serde_yaml::from_reader(file)
            .with_context(|| format!("Parsing YAML config file '{}'", path))?;
        Ok(config)
    }
}
