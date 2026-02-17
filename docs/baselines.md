# Baseline Bootstrap Watches

Watches to seed the system with enough listings to build price baselines.
Each product key needs at least 10 samples before the price scoring factor
activates (40% of the composite score). Thresholds are set to 60 during
bootstrap — raise them to 75-80 once baselines are populated.

Run `spt baselines list` periodically to check which product keys have
reached the 10-sample minimum.

## RAM

The highest-volume server component on eBay. DDR4 ECC RDIMMs in 16GB, 32GB,
and 64GB are the most common.

```bash
spt watches create \
  --name "DDR4 ECC 16GB" \
  --query "DDR4 ECC 16GB RDIMM server memory" \
  --type ram \
  --threshold 60

spt watches create \
  --name "DDR4 ECC 32GB" \
  --query "DDR4 ECC 32GB RDIMM server memory" \
  --type ram \
  --threshold 60

spt watches create \
  --name "DDR4 ECC 64GB" \
  --query "DDR4 ECC 64GB RDIMM server memory" \
  --type ram \
  --threshold 60

spt watches create \
  --name "DDR4 ECC 128GB" \
  --query "DDR4 ECC 128GB LRDIMM server memory" \
  --type ram \
  --threshold 60

spt watches create \
  --name "DDR5 ECC 32GB" \
  --query "DDR5 ECC 32GB RDIMM server memory" \
  --type ram \
  --threshold 60

spt watches create \
  --name "DDR5 ECC 64GB" \
  --query "DDR5 ECC 64GB RDIMM server memory" \
  --type ram \
  --threshold 60
```

## Drives

Enterprise SSDs and SAS drives in the most common capacities and form factors.

### Specific SAS 10K Models

These are the workhorses for Dell 14th-gen (R640, R740xd, R940) and similar
platforms. The Seagate ST1800MM0129 comes in both 512e and 4Kn sector formats —
512e is what you want for broad compatibility with PERC H730P/H740P controllers.

```bash
# HGST/WD Ultrastar C10K1800 — 1.8TB 10K SAS 2.5"
spt watches create \
  --name "HGST C10K1800 1.8TB" \
  --query "HUC101818CS4200 1.8TB SAS 10K 2.5" \
  --type drive \
  --threshold 60

spt watches create \
  --name "Ultrastar C10K1800 1.8TB" \
  --query "Ultrastar C10K1800 1.8TB SAS 10K 12Gb" \
  --type drive \
  --threshold 60

# Seagate Exos 10E2400 ST1800MM0129 — 1.8TB 10K SAS 2.5"
# 512e variant for PERC controller compatibility (R640/R740xd/R940)
spt watches create \
  --name "Seagate ST1800MM0129 512e" \
  --query "ST1800MM0129 1.8TB SAS 512e" \
  --type drive \
  --threshold 60

spt watches create \
  --name "Seagate Exos 10E2400 1.8TB" \
  --query "Seagate Exos 10E2400 1.8TB SAS 10K 2.5" \
  --type drive \
  --threshold 60

# Seagate Exos 10E300 ST1200MM0099 — 1.2TB 10K SAS 2.5"
# 12Gbps SAS, 128MB cache — common R630/R730 pull
spt watches create \
  --name "Seagate ST1200MM0099 1.2TB" \
  --query "ST1200MM0099 1.2TB SAS 10K 12Gb" \
  --type drive \
  --threshold 60

spt watches create \
  --name "Seagate Exos 10E300 1.2TB" \
  --query "Seagate Exos 10E300 1.2TB SAS 10K 2.5" \
  --type drive \
  --threshold 60
```

### Broader Drive Watches

Generic queries to build baselines across capacity/interface combinations.

```bash
# Enterprise SATA SSDs
spt watches create \
  --name "960GB SATA SSD Enterprise" \
  --query "960GB SATA SSD enterprise server 2.5" \
  --type drive \
  --threshold 60

spt watches create \
  --name "1.92TB SATA SSD Enterprise" \
  --query "1.92TB SATA SSD enterprise server 2.5" \
  --type drive \
  --threshold 60

spt watches create \
  --name "3.84TB SATA SSD Enterprise" \
  --query "3.84TB SATA SSD enterprise server 2.5" \
  --type drive \
  --threshold 60

# NVMe SSDs
spt watches create \
  --name "1.6TB NVMe U.2 SSD" \
  --query "1.6TB NVMe U.2 SSD enterprise server" \
  --type drive \
  --threshold 60

# SAS HDDs
spt watches create \
  --name "1.2TB SAS 10K 2.5" \
  --query "1.2TB SAS 10K 2.5 server hard drive" \
  --type drive \
  --threshold 60

spt watches create \
  --name "600GB SAS 15K 2.5" \
  --query "600GB SAS 15K 2.5 server hard drive" \
  --type drive \
  --threshold 60

# SATA HDDs (bulk storage)
spt watches create \
  --name "4TB SATA 3.5 7200" \
  --query "4TB SATA 3.5 7200RPM server hard drive" \
  --type drive \
  --threshold 60

spt watches create \
  --name "8TB SATA 3.5 7200" \
  --query "8TB SATA 3.5 7200RPM server hard drive" \
  --type drive \
  --threshold 60
```

## Servers

Popular 1U and 2U rack servers. These generate the most diverse product keys,
so broader queries help.

```bash
# Dell
spt watches create \
  --name "Dell R630" \
  --query "Dell PowerEdge R630" \
  --type server \
  --threshold 60

spt watches create \
  --name "Dell R640" \
  --query "Dell PowerEdge R640" \
  --type server \
  --threshold 60

spt watches create \
  --name "Dell R730" \
  --query "Dell PowerEdge R730" \
  --type server \
  --threshold 60

spt watches create \
  --name "Dell R730xd" \
  --query "Dell PowerEdge R730xd" \
  --type server \
  --threshold 60

spt watches create \
  --name "Dell R740" \
  --query "Dell PowerEdge R740" \
  --type server \
  --threshold 60

spt watches create \
  --name "Dell R740xd" \
  --query "Dell PowerEdge R740xd" \
  --type server \
  --threshold 60

# HP
spt watches create \
  --name "HP DL380 Gen9" \
  --query "HP ProLiant DL380 Gen9" \
  --type server \
  --threshold 60

spt watches create \
  --name "HP DL380 Gen10" \
  --query "HP ProLiant DL380 Gen10" \
  --type server \
  --threshold 60

spt watches create \
  --name "HP DL360 Gen9" \
  --query "HP ProLiant DL360 Gen9" \
  --type server \
  --threshold 60

spt watches create \
  --name "HP DL360 Gen10" \
  --query "HP ProLiant DL360 Gen10" \
  --type server \
  --threshold 60
```

## CPUs

High-volume Xeon models that appear frequently on eBay.

```bash
# Broadwell-EP (v4)
spt watches create \
  --name "Xeon E5-2680 v4" \
  --query "Intel Xeon E5-2680 v4 processor" \
  --type cpu \
  --threshold 60

spt watches create \
  --name "Xeon E5-2690 v4" \
  --query "Intel Xeon E5-2690 v4 processor" \
  --type cpu \
  --threshold 60

spt watches create \
  --name "Xeon E5-2660 v4" \
  --query "Intel Xeon E5-2660 v4 processor" \
  --type cpu \
  --threshold 60

# Skylake-SP (Gold/Platinum)
spt watches create \
  --name "Xeon Gold 6130" \
  --query "Intel Xeon Gold 6130 processor" \
  --type cpu \
  --threshold 60

spt watches create \
  --name "Xeon Gold 6148" \
  --query "Intel Xeon Gold 6148 processor" \
  --type cpu \
  --threshold 60

spt watches create \
  --name "Xeon Gold 6248" \
  --query "Intel Xeon Gold 6248 processor" \
  --type cpu \
  --threshold 60

spt watches create \
  --name "Xeon Platinum 8160" \
  --query "Intel Xeon Platinum 8160 processor" \
  --type cpu \
  --threshold 60

# EPYC
spt watches create \
  --name "EPYC 7302" \
  --query "AMD EPYC 7302 processor" \
  --type cpu \
  --threshold 60

spt watches create \
  --name "EPYC 7402" \
  --query "AMD EPYC 7402 processor" \
  --type cpu \
  --threshold 60
```

## NICs

Common 10GbE and 25GbE cards that show up in homelab and enterprise deals.

```bash
spt watches create \
  --name "Mellanox ConnectX-4 25GbE" \
  --query "Mellanox ConnectX-4 25GbE SFP28" \
  --type nic \
  --threshold 60

spt watches create \
  --name "Mellanox ConnectX-3 10GbE" \
  --query "Mellanox ConnectX-3 10GbE SFP+" \
  --type nic \
  --threshold 60

spt watches create \
  --name "Intel X710 10GbE" \
  --query "Intel X710 10GbE SFP+ dual port" \
  --type nic \
  --threshold 60

spt watches create \
  --name "Intel X520 10GbE" \
  --query "Intel X520-DA2 10GbE SFP+" \
  --type nic \
  --threshold 60

spt watches create \
  --name "Broadcom 57810 10GbE" \
  --query "Broadcom 57810 10GbE SFP+ dual port" \
  --type nic \
  --threshold 60
```

## After Bootstrap

Once baselines are populated (check with `spt baselines list`), raise the
thresholds on watches you care about:

```bash
# Example: raise a watch threshold to 80
curl -s -X PUT http://localhost:8080/api/v1/watches/<watch-id> \
  -H "Content-Type: application/json" \
  -d '{"score_threshold": 80}'
```

Disable or delete bootstrap watches you no longer need:

```bash
spt watches disable <watch-id>
spt watches delete <watch-id>
```
