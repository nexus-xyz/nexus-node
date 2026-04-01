use std::sync::Arc;

use alloy_consensus::Header;
use alloy_evm::{
    FromRecoveredTx, FromTxWithEncoded,
    eth::{EthBlockExecutionCtx, EthBlockExecutorFactory, spec::EthExecutorSpec},
};
use alloy_rpc_types_engine::ExecutionData;
use core::{convert::Infallible, fmt::Debug};
use reth_chainspec::{ChainSpec, EthChainSpec};
use reth_ethereum_forks::Hardforks;
use reth_ethereum_primitives::{Block, EthPrimitives, TransactionSigned};
use reth_evm::{
    ConfigureEngineEvm, ConfigureEvm, EvmEnv, EvmFactory, NextBlockEnvAttributes, TransactionEnv,
    precompiles::PrecompilesMap,
};
use reth_evm_ethereum::{EthBlockAssembler, EthEvmConfig, RethReceiptBuilder};
use reth_primitives_traits::{SealedBlock, SealedHeader};
use revm::primitives::hardfork::SpecId;

use crate::{block::NexusEthBlockExecutorFactory, evm::NexusEthEvmFactory};

/// Wraps [`EthEvmConfig`] with a custom `executor_factory` field. This creates two different
/// block factories, as one exists in `inner`. The latter provides a `spec()` method used in
/// `ConfigureEvm`.
#[derive(Debug, Clone)]
pub struct NexusEthEvmConfig<C = ChainSpec, EvmFactory = NexusEthEvmFactory> {
    /// Custom Factory [`EthBlockExecutorFactory`].
    executor_factory: NexusEthBlockExecutorFactory<RethReceiptBuilder, Arc<C>, EvmFactory>,
    /// Default EthEvmConfig with an `executor_factory` that goes unused.
    inner: EthEvmConfig<C, EvmFactory>,
}

impl<ChainSpec, EvmFactory: Clone> NexusEthEvmConfig<ChainSpec, EvmFactory> {
    /// Creates a new Ethereum EVM configuration with the given chain spec and EVM factory.
    pub fn new_with_evm_factory(chain_spec: Arc<ChainSpec>, evm_factory: EvmFactory) -> Self {
        let inner = EthEvmConfig {
            block_assembler: EthBlockAssembler::new(chain_spec.clone()),
            executor_factory: EthBlockExecutorFactory::new(
                RethReceiptBuilder::default(),
                chain_spec.clone(),
                evm_factory.clone(),
            ),
        };

        NexusEthEvmConfig {
            executor_factory: NexusEthBlockExecutorFactory::new(
                RethReceiptBuilder::default(),
                chain_spec,
                evm_factory,
            ),
            inner,
        }
    }
}

impl<ChainSpec, EvmF> ConfigureEvm for NexusEthEvmConfig<ChainSpec, EvmF>
where
    ChainSpec: EthExecutorSpec + EthChainSpec<Header = Header> + Hardforks + 'static,
    EvmF: EvmFactory<
            Tx: TransactionEnv
                    + FromRecoveredTx<TransactionSigned>
                    + FromTxWithEncoded<TransactionSigned>,
            Spec = SpecId,
            BlockEnv = revm::context::BlockEnv,
            Precompiles = PrecompilesMap,
        > + Clone
        + Debug
        + Send
        + Sync
        + Unpin
        + 'static,
{
    type Primitives = EthPrimitives;
    type Error = Infallible;
    type NextBlockEnvCtx = NextBlockEnvAttributes;
    type BlockExecutorFactory =
        NexusEthBlockExecutorFactory<RethReceiptBuilder, Arc<ChainSpec>, EvmF>;
    type BlockAssembler = EthBlockAssembler<ChainSpec>;

    fn block_executor_factory(&self) -> &Self::BlockExecutorFactory {
        &self.executor_factory
    }

    fn block_assembler(&self) -> &Self::BlockAssembler {
        &self.inner.block_assembler
    }

    fn evm_env(&self, header: &Header) -> Result<EvmEnv, Self::Error> {
        self.inner.evm_env(header)
    }

    fn next_evm_env(
        &self,
        parent: &Header,
        attributes: &NextBlockEnvAttributes,
    ) -> Result<EvmEnv, Self::Error> {
        self.inner.next_evm_env(parent, attributes)
    }

    fn context_for_block<'a>(
        &self,
        block: &'a SealedBlock<Block>,
    ) -> Result<EthBlockExecutionCtx<'a>, Self::Error> {
        self.inner.context_for_block(block)
    }

    fn context_for_next_block(
        &self,
        parent: &SealedHeader,
        attributes: Self::NextBlockEnvCtx,
    ) -> Result<EthBlockExecutionCtx<'_>, Self::Error> {
        self.inner.context_for_next_block(parent, attributes)
    }
}

impl<ChainSpec, EvmF> ConfigureEngineEvm<ExecutionData> for NexusEthEvmConfig<ChainSpec, EvmF>
where
    ChainSpec: EthExecutorSpec + EthChainSpec<Header = Header> + Hardforks + 'static,
    EvmF: EvmFactory<
            Tx: TransactionEnv
                    + FromRecoveredTx<TransactionSigned>
                    + FromTxWithEncoded<TransactionSigned>,
            Spec = SpecId,
            BlockEnv = revm::context::BlockEnv,
            Precompiles = PrecompilesMap,
        > + Clone
        + Debug
        + Send
        + Sync
        + Unpin
        + 'static,
{
    fn evm_env_for_payload(
        &self,
        payload: &ExecutionData,
    ) -> Result<reth_evm::EvmEnvFor<Self>, Self::Error> {
        self.inner.evm_env_for_payload(payload)
    }

    fn context_for_payload<'a>(
        &self,
        payload: &'a ExecutionData,
    ) -> Result<reth_evm::ExecutionCtxFor<'a, Self>, Self::Error> {
        self.inner.context_for_payload(payload)
    }

    fn tx_iterator_for_payload(
        &self,
        payload: &ExecutionData,
    ) -> Result<impl reth_evm::ExecutableTxIterator<Self>, Self::Error> {
        self.inner.tx_iterator_for_payload(payload)
    }
}
