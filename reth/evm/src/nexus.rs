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

/// Fork activation times for Reth (unix seconds). Keep `fork_timings` aligned with Cosmos
/// `forks.prague_timestamp` / `forks.osaka_timestamp` (Engine V4 / V5 API tiers) and the payload
/// timestamps the keeper computes.
#[derive(Debug, Clone, Deserialize, Default)]
pub struct ForkTimings {
    // ETH Forks
    #[serde(default)]
    pub prague_time: Option<u64>,
    #[serde(default)]
    pub osaka_time: Option<u64>,

    // Nexus Forks
    #[serde(default)]
    pub v0_time: Option<u64>,
}

impl ForkTimings {
    /// Validates that fork timestamps are consistent (e.g. Osaka must be after Prague when both are set).
    pub fn validate(&self) -> eyre::Result<()> {
        if let (Some(prague), Some(osaka)) = (self.prague_time, self.osaka_time)
            && osaka <= prague
        {
            eyre::bail!(
                "osaka_time ({}) must be greater than prague_time ({}) when both are set",
                osaka,
                prague
            );
        }
        Ok(())
    }
}

impl NexusConfig {
    pub fn apply_forks(&self, chain_spec: &Arc<ChainSpec>) -> eyre::Result<ChainSpec> {
        self.fork_timings.validate()?;
        let mut builder = ChainSpecBuilder::from(chain_spec);

        if let Some(prague_time) = self.fork_timings.prague_time {
            builder = builder.without_fork(EthereumHardfork::Prague).with_fork(
                EthereumHardfork::Prague,
                ForkCondition::Timestamp(prague_time),
            );
            tracing::debug!("Applying Prague fork at timestamp {}", prague_time);
        }

        if let Some(osaka_time) = self.fork_timings.osaka_time {
            builder = builder.without_fork(EthereumHardfork::Osaka).with_fork(
                EthereumHardfork::Osaka,
                ForkCondition::Timestamp(osaka_time),
            );
            tracing::debug!("Applying Osaka fork at timestamp {}", osaka_time);
        }

        if let Some(v0_time) = self.fork_timings.v0_time {
            builder = builder
                .without_fork(Nexus::V0)
                .with_fork(Nexus::V0, ForkCondition::Timestamp(v0_time));
            tracing::debug!("Applying Nexus V0 fork at timestamp {}", v0_time);
        }

        Ok(builder.build())
    }

    /// Parse a YAML configuration file from a path
    pub fn parse_nexus_config(path: &str) -> eyre::Result<NexusConfig> {
        let file =
            std::fs::File::open(path).with_context(|| format!("Opening config file '{}'", path))?;

        let config: NexusConfig = serde_yaml::from_reader(file)
            .with_context(|| format!("Parsing YAML config file '{}'", path))?;
        config.fork_timings.validate()?;
        Ok(config)
    }
}
