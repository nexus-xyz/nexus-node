# Nexus External Validators

Resources for validators joining the Nexus network.

## Docker image

Nexus publishes signed validator node images to DockerHub:

```
nexusxyz/cosmos:<tag>
```

## Verifying the image

Images are signed with Cosign via GitHub Actions OIDC. Verify before running:

```bash
cosign verify \
  --certificate-identity-regexp="https://github.com/nexus-xyz/nexus/.github/workflows/eng-chain-cosmos-build.yml@refs/.*" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  nexusxyz/cosmos:<tag> | jq .
```

Replace `<tag>` with the version you intend to run.

## Running a validator node

```bash
docker run -d \
  --name nexus-validator \
  -p 26656:26656 \
  -p 26657:26657 \
  -p 1317:1317 \
  -p 9090:9090 \
  -v /path/to/your/node/data:/home/l1 \
  nexusxyz/cosmos:<tag>
```

The node data directory is mounted at `/home/l1` inside the container. This is where chain data, keys, and config live across restarts.

## Source code

The `eng/chain/cosmos/` directory in this repo contains the source used to build each published image. It is provided for auditability — validators can verify that the image they are running matches the published source.

## Support

For onboarding support, contact the Nexus team.
