use std::ops::Deref;

use alloy_consensus::{Transaction, TxReceipt};
use alloy_eips::Encodable2718;
use alloy_evm::{
    Database, Evm, EvmFactory, FromRecoveredTx, FromTxWithEncoded,
    block::{
        BlockExecutionError, BlockExecutionResult, BlockExecutor, BlockExecutorFactory,
        BlockExecutorFor, CommitChanges, ExecutableTx, OnStateHook,
    },
    eth::{
        EthBlockExecutionCtx, EthBlockExecutor, EthBlockExecutorFactory,
        receipt_builder::{AlloyReceiptBuilder, ReceiptBuilder},
        spec::{EthExecutorSpec, EthSpec},
    },
};
use alloy_primitives::Log;
use reth_tracing::tracing;
use revm::{
    Inspector,
    context::Block,
    context::result::{ExecutionResult, ResultAndState},
    database::State,
};

use crate::evm::NexusEthEvmFactory;

pub struct NexusEthBlockExecutor<'a, Evm, Spec, R: ReceiptBuilder> {
    inner: EthBlockExecutor<'a, Evm, Spec, R>,
}

impl<'db, DB, E, Spec, R> BlockExecutor for NexusEthBlockExecutor<'_, E, Spec, R>
where
    DB: Database + 'db,
    E: Evm<
            DB = &'db mut State<DB>,
            Tx: FromRecoveredTx<R::Transaction> + FromTxWithEncoded<R::Transaction>,
        >,
    Spec: EthExecutorSpec,
    R: ReceiptBuilder<Transaction: Transaction + Encodable2718, Receipt: TxReceipt<Log = Log>>,
{
    type Transaction = R::Transaction;
    type Receipt = R::Receipt;
    type Evm = E;

    fn apply_pre_execution_changes(&mut self) -> Result<(), BlockExecutionError> {
        let block_number = self.inner.evm.block().number().saturating_to::<u64>();
        let block_timestamp = self.inner.evm.block().timestamp().saturating_to::<u64>();

        tracing::debug!(
            target: "nexus::block",
            "Block execution starting: block={} timestamp={}",
            block_number,
            block_timestamp
        );

        self.inner.apply_pre_execution_changes()
    }

    fn execute_transaction_with_commit_condition(
        &mut self,
        tx: impl ExecutableTx<Self>,
        f: impl FnOnce(&ExecutionResult<<Self::Evm as Evm>::HaltReason>) -> CommitChanges,
    ) -> Result<Option<u64>, BlockExecutionError> {
        let block_number = self.inner.evm.block().number().saturating_to::<u64>();
        let tx_hash = tx.tx().trie_hash().to_vec();

        tracing::debug!(
            target: "nexus::block",
            "Transaction execution starting: block={} tx={:?}",
            block_number,
            tx_hash
        );

        self.inner.execute_transaction_with_commit_condition(tx, f)
    }

    fn finish(self) -> Result<(Self::Evm, BlockExecutionResult<R::Receipt>), BlockExecutionError> {
        let result = self.inner.finish()?;

        let block = result.0.block();
        let block_number = block.number().saturating_to::<u64>();
        let block_timestamp = block.timestamp().saturating_to::<u64>();

        tracing::debug!(
            target: "nexus::block",
            "Block execution finished: block={} timestamp={}",
            block_number,
            block_timestamp
        );

        Ok(result)
    }

    fn set_state_hook(&mut self, hook: Option<Box<dyn OnStateHook>>) {
        self.inner.set_state_hook(hook)
    }

    fn evm_mut(&mut self) -> &mut Self::Evm {
        self.inner.evm_mut()
    }

    fn evm(&self) -> &Self::Evm {
        self.inner.evm()
    }

    fn receipts(&self) -> &[Self::Receipt] {
        self.inner.receipts()
    }

    fn execute_transaction_without_commit(
        &mut self,
        tx: impl ExecutableTx<Self>,
    ) -> Result<ResultAndState<<Self::Evm as Evm>::HaltReason>, BlockExecutionError> {
        self.inner.execute_transaction_without_commit(tx)
    }

    fn commit_transaction(
        &mut self,
        result: ResultAndState<<Self::Evm as Evm>::HaltReason>,
        tx: impl ExecutableTx<Self>,
    ) -> Result<u64, BlockExecutionError> {
        self.inner.commit_transaction(result, tx)
    }
}

#[derive(Debug, Clone, Default, Copy)]
pub struct NexusEthBlockExecutorFactory<
    R = AlloyReceiptBuilder,
    Spec = EthSpec,
    EvmFactory = NexusEthEvmFactory,
>(EthBlockExecutorFactory<R, Spec, EvmFactory>);

impl<R, Spec, EvmF> Deref for NexusEthBlockExecutorFactory<R, Spec, EvmF> {
    type Target = EthBlockExecutorFactory<R, Spec, EvmF>;

    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

impl<R, Spec, EvmF> NexusEthBlockExecutorFactory<R, Spec, EvmF> {
    /// Creates a new [`NexusEthBlockExecutorFactory`] with the given spec, [`EvmFactory`], and
    /// [`ReceiptBuilder`].
    pub const fn new(receipt_builder: R, spec: Spec, evm_factory: EvmF) -> Self {
        let inner = EthBlockExecutorFactory::new(receipt_builder, spec, evm_factory);
        Self(inner)
    }
}

impl<R, Spec, EvmF> BlockExecutorFactory for NexusEthBlockExecutorFactory<R, Spec, EvmF>
where
    R: ReceiptBuilder<Transaction: Transaction + Encodable2718, Receipt: TxReceipt<Log = Log>>,
    Spec: EthExecutorSpec,
    EvmF: EvmFactory<Tx: FromRecoveredTx<R::Transaction> + FromTxWithEncoded<R::Transaction>>,
    Self: 'static,
{
    type EvmFactory = EvmF;
    type ExecutionCtx<'a> = EthBlockExecutionCtx<'a>;
    type Transaction = R::Transaction;
    type Receipt = R::Receipt;

    fn evm_factory(&self) -> &Self::EvmFactory {
        self.0.evm_factory()
    }

    fn create_executor<'a, DB, I>(
        &'a self,
        evm: EvmF::Evm<&'a mut State<DB>, I>,
        ctx: Self::ExecutionCtx<'a>,
    ) -> impl BlockExecutorFor<'a, Self, DB, I>
    where
        DB: Database + 'a,
        I: Inspector<EvmF::Context<&'a mut State<DB>>> + 'a,
    {
        let inner = EthBlockExecutor::new(evm, ctx, self.spec(), self.receipt_builder());
        NexusEthBlockExecutor { inner }
    }
}
