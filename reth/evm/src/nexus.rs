use std::sync::Arc;

use eyre::Context;
use reth_chainspec::{ChainSpec, ChainSpecBuilder, EthereumHardfork, ForkCondition};
use reth_tracing::tracing;
use serde::Deserialize;

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

#[cfg(test)]
mod tests {
    use super::*;
    use reth_chainspec::ChainSpecBuilder;
    use std::io::Write;
    use tempfile::NamedTempFile;

    fn base_chain_spec() -> Arc<ChainSpec> {
        Arc::new(ChainSpecBuilder::mainnet().build())
    }

    fn write_yaml(contents: &str) -> NamedTempFile {
        let mut file = NamedTempFile::new().expect("create temp yaml");
        file.write_all(contents.as_bytes())
            .expect("write temp yaml");
        file
    }

    // ---- ForkTimings::validate ----

    #[test]
    fn validate_both_unset_is_ok() {
        let timings = ForkTimings {
            prague_time: None,
            osaka_time: None,
        };
        timings
            .validate()
            .expect("no timings means nothing to validate");
    }

    #[test]
    fn validate_only_prague_is_ok() {
        let timings = ForkTimings {
            prague_time: Some(100),
            osaka_time: None,
        };
        timings
            .validate()
            .expect("a single fork timing is always consistent");
    }

    #[test]
    fn validate_only_osaka_is_ok() {
        let timings = ForkTimings {
            prague_time: None,
            osaka_time: Some(200),
        };
        timings
            .validate()
            .expect("a single fork timing is always consistent");
    }

    #[test]
    fn validate_osaka_after_prague_is_ok() {
        let timings = ForkTimings {
            prague_time: Some(100),
            osaka_time: Some(200),
        };
        timings
            .validate()
            .expect("strict ordering is the happy path");
    }

    #[test]
    fn validate_rejects_osaka_equal_to_prague() {
        // Boundary: equal timestamps must fail because the check is `<=`.
        let timings = ForkTimings {
            prague_time: Some(100),
            osaka_time: Some(100),
        };
        let _ = timings
            .validate()
            .expect_err("equal timestamps must be rejected");
    }

    #[test]
    fn validate_rejects_osaka_before_prague() {
        let timings = ForkTimings {
            prague_time: Some(200),
            osaka_time: Some(100),
        };
        let _ = timings
            .validate()
            .expect_err("osaka before prague must be rejected");
    }

    // ---- NexusConfig::apply_forks ----

    #[test]
    fn apply_forks_with_empty_timings_preserves_base_spec() {
        let base = base_chain_spec();
        let cfg = NexusConfig {
            fork_timings: ForkTimings::default(),
        };
        let out = cfg
            .apply_forks(&base)
            .expect("empty timings must not error");
        assert_eq!(
            out.hardforks.fork(EthereumHardfork::Prague),
            base.hardforks.fork(EthereumHardfork::Prague),
        );
        assert_eq!(
            out.hardforks.fork(EthereumHardfork::Osaka),
            base.hardforks.fork(EthereumHardfork::Osaka),
        );
    }

    #[test]
    fn apply_forks_sets_only_prague_when_only_prague_is_given() {
        let base = base_chain_spec();
        let cfg = NexusConfig {
            fork_timings: ForkTimings {
                prague_time: Some(1_700_000_000),
                osaka_time: None,
            },
        };
        let out = cfg.apply_forks(&base).expect("valid timings must apply");
        assert_eq!(
            out.hardforks.fork(EthereumHardfork::Prague),
            ForkCondition::Timestamp(1_700_000_000),
        );
        assert_eq!(
            out.hardforks.fork(EthereumHardfork::Osaka),
            base.hardforks.fork(EthereumHardfork::Osaka),
        );
    }

    #[test]
    fn apply_forks_sets_only_osaka_when_only_osaka_is_given() {
        let base = base_chain_spec();
        let cfg = NexusConfig {
            fork_timings: ForkTimings {
                prague_time: None,
                osaka_time: Some(1_800_000_000),
            },
        };
        let out = cfg.apply_forks(&base).expect("valid timings must apply");
        assert_eq!(
            out.hardforks.fork(EthereumHardfork::Osaka),
            ForkCondition::Timestamp(1_800_000_000),
        );
        assert_eq!(
            out.hardforks.fork(EthereumHardfork::Prague),
            base.hardforks.fork(EthereumHardfork::Prague),
        );
    }

    #[test]
    fn apply_forks_sets_both_when_both_are_given() {
        let base = base_chain_spec();
        let cfg = NexusConfig {
            fork_timings: ForkTimings {
                prague_time: Some(1_700_000_000),
                osaka_time: Some(1_800_000_000),
            },
        };
        let out = cfg.apply_forks(&base).expect("valid timings must apply");
        assert_eq!(
            out.hardforks.fork(EthereumHardfork::Prague),
            ForkCondition::Timestamp(1_700_000_000),
        );
        assert_eq!(
            out.hardforks.fork(EthereumHardfork::Osaka),
            ForkCondition::Timestamp(1_800_000_000),
        );
    }

    #[test]
    fn apply_forks_propagates_validation_error() {
        let base = base_chain_spec();
        let cfg = NexusConfig {
            fork_timings: ForkTimings {
                prague_time: Some(200),
                osaka_time: Some(100),
            },
        };
        let _ = cfg
            .apply_forks(&base)
            .expect_err("invalid ordering must surface from validate()");
    }

    // ---- NexusConfig::parse_nexus_config ----

    #[test]
    fn parse_yaml_with_valid_fork_timings_roundtrips() {
        let yaml = "fork_timings:\n  prague_time: 1700000000\n  osaka_time: 1800000000\n";
        let file = write_yaml(yaml);
        let cfg = NexusConfig::parse_nexus_config(file.path().to_str().unwrap())
            .expect("valid config must parse");
        assert_eq!(cfg.fork_timings.prague_time, Some(1_700_000_000));
        assert_eq!(cfg.fork_timings.osaka_time, Some(1_800_000_000));
    }

    #[test]
    fn parse_yaml_without_fork_timings_uses_defaults() {
        let yaml = "{}\n";
        let file = write_yaml(yaml);
        let cfg = NexusConfig::parse_nexus_config(file.path().to_str().unwrap())
            .expect("missing fork_timings is allowed via serde default");
        assert_eq!(cfg.fork_timings.prague_time, None);
        assert_eq!(cfg.fork_timings.osaka_time, None);
    }

    #[test]
    fn parse_yaml_with_empty_fork_timings_uses_defaults() {
        let yaml = "fork_timings: {}\n";
        let file = write_yaml(yaml);
        let cfg = NexusConfig::parse_nexus_config(file.path().to_str().unwrap())
            .expect("empty fork_timings must fall back to None/None");
        assert_eq!(cfg.fork_timings.prague_time, None);
        assert_eq!(cfg.fork_timings.osaka_time, None);
    }

    #[test]
    fn parse_yaml_rejects_syntactically_invalid_input() {
        let yaml = "fork_timings:\n  prague_time: : :\n";
        let file = write_yaml(yaml);
        let _ = NexusConfig::parse_nexus_config(file.path().to_str().unwrap())
            .expect_err("syntactically broken YAML must fail to parse");
    }

    #[test]
    fn parse_yaml_runs_validation_after_parse() {
        // Parse succeeds but validate() must reject osaka <= prague.
        let yaml = "fork_timings:\n  prague_time: 200\n  osaka_time: 100\n";
        let file = write_yaml(yaml);
        let _ = NexusConfig::parse_nexus_config(file.path().to_str().unwrap())
            .expect_err("validation must run after deserialisation");
    }
}
