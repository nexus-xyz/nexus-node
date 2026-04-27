use std::sync::Arc;

use clap::{Args, Parser};
use reth::cli::Cli;
use reth_node_ethereum::{EthereumAddOns, EthereumNode};

use crate::{evm::NexusEthereumExecutorBuilder, forks::NexusChainSpecParser, nexus::NexusConfig};

mod block;
mod config;
mod evm;
mod forks;
mod nexus;

#[derive(Debug, Clone, Args)]
pub struct NexusArgs {
    /// Path to the Nexus Reth configuration file. See `nexus-config.yaml`.
    #[arg(
        long = "nexus-config",
        value_name = "PATH",
        value_parser = NexusConfig::parse_nexus_config
    )]
    pub nexus_config: NexusConfig,
}

pub fn main() -> eyre::Result<()> {
    Cli::<NexusChainSpecParser, NexusArgs>::parse().run(|mut builder, args| async move {
        let chain_spec = args.nexus_config.apply_forks(&builder.config().chain)?;
        builder.config_mut().chain = Arc::new(chain_spec);

        let handle = builder
            .with_types::<EthereumNode>()
            .with_components(EthereumNode::components().executor(NexusEthereumExecutorBuilder))
            .with_add_ons(EthereumAddOns::default())
            .launch()
            .await?;

        handle.wait_for_node_exit().await
    })?;

    Ok(())
}
