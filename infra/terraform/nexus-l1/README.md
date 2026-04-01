# Nexus L1 Terraform Infrastructure

Simplified Terraform configuration that manages firewall rules for all environments (staging, testnet, mainnet) in a single apply.

## Architecture

This infrastructure manages environment-specific firewall rules for validators across all environments:

- **Data Sources**: Reads existing network and GKE cluster info (doesn't create them)
- **Environment Configs**: Each environment's validator IPs defined in code
- **Firewall Rules**: Creates one firewall rule per environment using `for_each`
- **Single Apply**: One `terraform apply` deploys all environments

### Key Components

- **VPC Network** (referenced via data source)
- **Subnets** (us-central1, us-west1)
- **GKE Cluster** (shared across environments)
- **Environment-specific firewall rules**
- **Validator allowlist configurations**

## Directory Structure

```
nexus-l1/
├── main.tf          # Network data sources + firewall resources
├── environments.tf  # Environment-specific parameters
├── variables.tf     # Project-level variables
├── versions.tf      # Terraform and provider versions
├── backend.tf       # GCS backend configuration
├── outputs.tf       # Outputs for created resources
└── modules/         # Helper functions
```

## Quick Start

### Initialize

```bash
cd infra/terraform/nexus-l1
terraform init
```

### Plan and Apply

```bash
terraform plan
terraform apply
```

This will create firewall rules for **all environments** (staging, testnet, mainnet) in a single apply.

## Configuration

### Adding Validator IPs

Edit `environments.tf` and add IPs to the appropriate environment:

```hcl
locals {
  environments = {
    staging = {
      ingress_to_validator_allowlist_source_ranges = [
        "34.136.57.38/32",
        "1.2.3.4/32",  # Add new IP here
      ]
    }
    testnet = {
      ingress_to_validator_allowlist_source_ranges = [
        # Testnet validator IPs
      ]
    }
    mainnet = {
      ingress_to_validator_allowlist_source_ranges = [
        # Mainnet validator IPs
      ]
    }
  }
}
```

Then run `terraform apply` to update the firewall rules.

## How It Works

### Firewall Rules Created

For each environment, creates a rule named `allow-ingress-to-validators-{env}`:

- **Rule name**: `allow-ingress-to-validators-{environment}`
  - staging: `allow-ingress-to-validators-staging`
  - testnet: `allow-ingress-to-validators-testnet`
  - mainnet: `allow-ingress-to-validators-mainnet`
- **Source Ranges**: Subnet IPs + GKE pod/service CIDRs + environment-specific validator IPs
- **Target Tags**: `l1-validator-{env}`
  - staging: `l1-validator-staging`
  - testnet: `l1-validator-testnet`
  - mainnet: `l1-validator-mainnet`

The target tags (`l1-validator-{env}`) allow validator traffic via environment-specific firewall rules.

### Source Range Merging

The firewall module automatically combines:

1. **Shared network ranges** (GKE subnets, pod/service CIDRs)
2. **Environment-specific IPs** from `environments.tf`

This ensures:

- GKE nodes can always reach validators (shared ranges)
- Environment-specific validators can reach each other (env ranges)
- No duplication of shared ranges across environments

## Common Operations

### View Current State

```bash
terraform show
```

### View Outputs

```bash
terraform output
```

### Update Environment Configuration

```bash
# Edit the configuration
vim environments.tf

# Plan and apply changes
terraform plan
terraform apply
```

### Validate Configuration

```bash
terraform validate
```

### Format Code

```bash
terraform fmt -recursive
```

### Destroy All Resources (CAREFUL!)

```bash
terraform destroy
```

This will remove **all** firewall rules for all environments.

## Security Notes

- Firewall rules use target tags for environment isolation
- All CIDR ranges validated at plan time
- Environment-specific tags ensure proper isolation
- Shared network ranges (GKE) automatically included
- Each environment has distinct firewall rules

## State Management

### Local Backend (Default)

State file: `terraform.tfstate`

### Remote Backend (Recommended)

Configure GCS backend in `backend.tf` for production use.

## Troubleshooting

### Error: Network not found

Make sure the VPC network exists in the specified project. The configuration references existing network infrastructure via data sources.

```bash
# Verify network exists
gcloud compute networks list --project=your-project-id
```

### Firewall rules not applying

Check that:

1. Target tags on your VMs match the environment-specific pattern: `l1-validator-{env}`
2. VMs are in the correct VPC network
3. VMs have the correct network tags applied

```bash
# Check VM tags
gcloud compute instances describe VM_NAME --project=your-project-id --format="value(tags.items)"
```

### Error: Invalid CIDR range

Ensure all IPs in `environments.tf` are valid CIDR notation:

- Use `/32` for single IP addresses: `1.2.3.4/32`
- Validate format: `xxx.xxx.xxx.xxx/xx`

### View firewall rules

```bash
# List all firewall rules
gcloud compute firewall-rules list --project=your-project-id

# Describe specific rule
gcloud compute firewall-rules describe allow-ingress-to-validators-staging --project=your-project-id
```
