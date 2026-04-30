# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities privately to **security@nexus.xyz**.
Do not file a public GitHub issue for security problems.

Include in your report:

- A description of the vulnerability and its impact.
- The version (commit SHA or release tag) you reproduced against.
- Step-by-step reproduction instructions.
- Any proof-of-concept code, transactions, or payloads, where applicable.
- Whether the issue has been disclosed to anyone else.

We aim to acknowledge reports within **3 business days** and to follow up with
a triage assessment and tentative remediation timeline within **10 business
days**. Reports that include a clear reproduction and impact analysis tend to
move fastest.

## Scope

In scope:

- The `nexusd` binary built from `cosmos/` in this repository.
- The `nexus-evm` binary built from `reth/` in this repository.
- Consensus, execution, fee-policy, or upgrade-handling bugs that affect a
  network running these binaries.

Out of scope:

- Findings that only reproduce against unmaintained tags or development
  branches.
- Issues that require physical access to a validator's host or its key
  material.
- Denial-of-service against a single self-operated node running with
  non-recommended flags or non-default configuration.
- Issues in third-party dependencies that are already publicly disclosed and
  for which an upstream fix is available — please file those upstream.

## Coordinated disclosure

We coordinate public disclosure timing with the reporter and credit reporters
in the release notes unless they prefer to remain anonymous.
