use reth::revm::{
    Context, Inspector, MainBuilder, MainContext,
    context::{
        BlockEnv as RevmBlockEnv, TxEnv,
        result::{EVMError, HaltReason},
    },
    handler::EthPrecompiles,
    inspector::NoOpInspector,
    primitives::hardfork::SpecId,
};
use reth_chainspec::ChainSpec;
use reth_ethereum_primitives::EthPrimitives;
use reth_evm::{
    Database, EthEvm, EvmEnv, EvmFactory, eth::EthEvmContext, precompiles::PrecompilesMap,
};
use reth_node_api::{FullNodeTypes, NodeTypes};
use reth_node_builder::components::ExecutorBuilder;
use std::sync::Arc;

use crate::config::NexusEthEvmConfig;

#[derive(Debug, Clone)]
pub struct NexusEthEvmFactory {
    #[allow(dead_code)]
    chain_spec: Arc<ChainSpec>,
}

impl NexusEthEvmFactory {
    /// Create a new factory with the given chain specification
    pub fn new(chain_spec: Arc<ChainSpec>) -> Self {
        Self { chain_spec }
    }
}

impl EvmFactory for NexusEthEvmFactory {
    type Evm<DB: Database, I: Inspector<Self::Context<DB>>> = EthEvm<DB, I, Self::Precompiles>;
    type Context<DB: Database> = EthEvmContext<DB>;
    type Tx = TxEnv;
    type Error<DBError: std::error::Error + Send + Sync + 'static> = EVMError<DBError>;
    type HaltReason = HaltReason;
    type Spec = SpecId;
    type BlockEnv = RevmBlockEnv;
    type Precompiles = PrecompilesMap;

    fn create_evm<DB: Database>(
        &self,
        db: DB,
        env: EvmEnv<Self::Spec>,
    ) -> Self::Evm<DB, reth::revm::inspector::NoOpInspector> {
        let precompiles = PrecompilesMap::from_static(EthPrecompiles::default().precompiles);

        let evm = Context::mainnet()
            .with_db(db)
            .with_cfg(env.cfg_env)
            .with_block(env.block_env)
            .build_mainnet_with_inspector(NoOpInspector {})
            .with_precompiles(precompiles);

        EthEvm::new(evm, false)
    }

    fn create_evm_with_inspector<DB: Database, I: Inspector<Self::Context<DB>>>(
        &self,
        db: DB,
        input: EvmEnv<Self::Spec>,
        inspector: I,
    ) -> Self::Evm<DB, I> {
        EthEvm::new(
            self.create_evm(db, input)
                .into_inner()
                .with_inspector(inspector),
            true,
        )
    }
}

#[derive(Debug, Default, Clone, Copy)]
pub struct NexusEthereumExecutorBuilder;

impl<N> ExecutorBuilder<N> for NexusEthereumExecutorBuilder
where
    N: FullNodeTypes<Types: NodeTypes<ChainSpec = ChainSpec, Primitives = EthPrimitives>>,
{
    type EVM = NexusEthEvmConfig<ChainSpec, NexusEthEvmFactory>;

    async fn build_evm(
        self,
        ctx: &reth_node_builder::BuilderContext<N>,
    ) -> eyre::Result<Self::EVM> {
        let factory = NexusEthEvmFactory::new(ctx.chain_spec().clone());
        let config = NexusEthEvmConfig::new_with_evm_factory(ctx.chain_spec(), factory);
        Ok(config)
    }
}
