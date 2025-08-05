# ICF Cosmos Node Infrastructure


## 🎉 Welcome to Your Custom Cosmos Infrastructure

Your Cosmos blockchain infrastructure is now live and optimized. We have deployed and fine-tuned three high-performance full nodes (Neutron, dYdX, and Pryzm) specifically configured for your requirements.


## 📋 Available Endpoints

All endpoints are secured with TLS and accessible via:

### 🔗 Neutron
- **RPC:** [https://neutron-rpc.bryanlabs.net](https://neutron-rpc.bryanlabs.net)
- **WSS:** [wss://neutron-rpc.bryanlabs.net/websocket](wss://neutron-rpc.bryanlabs.net/websocket)
- **API:** [https://neutron-api.bryanlabs.net](https://neutron-api.bryanlabs.net)
- **GRPC:** `neutron-grpc.bryanlabs.net:443`

### 🔗 dYdX
- **RPC:** [https://dydx-rpc.bryanlabs.net](https://dydx-rpc.bryanlabs.net)
- **WSS:** [wss://dydx-rpc.bryanlabs.net/websocket](wss://dydx-rpc.bryanlabs.net/websocket)
- **API:** [https://dydx-api.bryanlabs.net](https://dydx-api.bryanlabs.net)
- **GRPC:** `dydx-grpc.bryanlabs.net:443`

### 🔗 Pryzm
- **RPC:** [https://pryzm-rpc.bryanlabs.net](https://pryzm-rpc.bryanlabs.net)
- **WSS:** [wss://pryzm-rpc.bryanlabs.net/websocket](wss://pryzm-rpc.bryanlabs.net/websocket)
- **API:** [https://pryzm-api.bryanlabs.net](https://pryzm-api.bryanlabs.net)
- **GRPC:** `pryzm-grpc.bryanlabs.net:443`

## 🏗️ Infrastructure Overview

### Deployed Chains

| Chain | SDK Version | Block Time | Status |
|-------|-------------|------------|--------|
| **Neutron** | v0.50.13-neutron-rpc | 1.1s | ✅ Optimized |
| **dYdX** | v0.50.6-0.20250708185419 | 1.1s | ✅ Optimized |
| **Pryzm** | v0.47.17 | 5.6s | ✅ Optimized |

### Enterprise Network Infrastructure

Your infrastructure is backed by enterprise-grade connectivity through our dedicated network infrastructure:

**Network Specifications:**
- **Autonomous System Number (ASN)**: [401711](https://whois.arin.net/rest/asn/AS401711) (ARIN registered)
- **Service Level Agreement**: 99.9% uptime guarantee
- **Port Speeds**: 1 Gbps to 100 Gbps capabilities
- **Routing**: Multi-path routing for enhanced redundancy

**Geographic Presence:**
- **Primary Peering Hub**: Ashburn, VA (Equinix IX)
- **Additional Locations**: Reston, VA • Baltimore, MD • Silver Spring, MD
- **Operations Center**: 24/7 monitoring from Silver Spring, MD

**Direct Peering Partners:**
- **Cloud Providers**: AWS, Google Cloud, Microsoft Azure, IBM Cloud
- **CDN Networks**: Cloudflare, Apple, Netflix, Meta, GitHub, Cisco
- **Internet Exchanges**: Equinix IX (Ashburn), NYIIX, FCIX
- **Satellite Connectivity**: SpaceX Starlink integration

**Technical Advantage**: Enterprise-grade connectivity powered by DACS-IX peering fabric ensures optimal performance and redundancy for blockchain infrastructure.

## 📊 Real-Time Monitoring & Analytics

Your infrastructure includes a comprehensive monitoring stack built on enterprise-grade observability tools, providing complete visibility into node performance, network activity, storage utilization, and service health.

### Monitoring Components

**Core Stack:**
- **Prometheus** - Metrics collection and time-series database
- **Grafana** - Interactive dashboards and visualization
- **Loki** - Log aggregation and search
- **Node Exporters** - System-level metrics (CPU, memory, disk, network)
- **CometBFT Exporters** - Blockchain-specific metrics (block height, sync status, peers)
- **HAProxy Exporter** - Load balancer and service health metrics
- **TopoLVM Exporter** - Storage volume monitoring

**Key Features:**
- **Dedicated Instance** - Your own isolated Grafana instance at `grafana-icf.bryanlabs.net`
- **Real-time Metrics** - 30-second collection intervals for most metrics
- **Interactive Dashboards** - Filter, search, and drill down into specific data
- **Historical Data** - Full retention for performance trending analysis

### 🎯 Key Dashboards

#### 1. [HAProxy Services Dashboard](https://grafana-icf.bryanlabs.net/d/haproxy-overview-internal-icf/ha-proxy-overview-internal-icf?orgId=1&from=now-6h&to=now&timezone=browser&var-backend=$__all&refresh=5s) **(Most Important)**
**Purpose:** Real-time service health monitoring
- ✅ **Service Status** - Up/down status for all RPC, API, and GRPC endpoints
- 📊 **Request Activity** - Live traffic and request patterns per service
- ⚠️ **Error Detection** - HTTP response codes and service issues
- 👥 **Usage Analytics** - Which endpoints are being used and by whom
- 🌐 **Session Tracking** - Connection data and bandwidth per service

#### 2. [Node Performance Dashboard](https://grafana-icf.bryanlabs.net/d/cometbft-nodes-internal-icf/cometbft-nodes-dashboard-internal-icf?orgId=1&from=now-6h&to=now&timezone=browser&var-chain_id=pryzm-1&var-instance=$__all&var-search_term=&refresh=30s)
**Purpose:** Blockchain node health and synchronization
- 🔄 **Sync Status** - Real-time block synchronization monitoring (positive = ahead, negative = behind)
- 📊 **Block Monitoring** - Block production, validation, and processing times
- 🔍 **Interactive Log Search** - Real-time log filtering and search capabilities
- 👥 **Peer Connectivity** - Network peer status and connection health
- 💫 **Transaction Monitoring** - Mempool status and transaction processing

**⚠️ Important:** Select only ONE chain ID at a time, or dashboard queries will fail

#### 3. [Storage Dashboard](https://grafana-icf.bryanlabs.net/d/topolvm-pvc-usage-icf/topolvm-and-pvc-usage-icf?orgId=1&from=now-6h&to=now&timezone=browser&var-pvc_filter=$__all&refresh=30s)
**Purpose:** Storage utilization and capacity planning
- 💾 **Total Capacity** - 11.6TB total storage across high-performance NVMe drives
- 📈 **Usage Trends** - Storage growth patterns per chain
- ⚡ **Performance Metrics** - I/O performance and throughput
- 📊 **Per-Chain Breakdown** - Storage allocation by blockchain

#### 4. [Network Bandwidth Dashboard](https://grafana-icf.bryanlabs.net/d/k8s-network-dashboard-icf/f09f9a80-kubernetes-network-dashboard-icf?orgId=1&from=now-6h&to=now&timezone=browser&var-namespace=fullnodes&refresh=5s)
**Purpose:** Network utilization monitoring
- 🌐 **Per-Chain Bandwidth** - Network usage by each blockchain
- 📡 **Peer Synchronization** - Data transfer for blockchain synchronization
- 📊 **Traffic Patterns** - Ingress/egress network patterns
- ⚡ **Real-time Updates** - 5-second refresh for live network monitoring

### 📈 Monitoring Benefits

**Proactive Issue Detection:**
- Sync lag alerts
- Service health monitoring with immediate visibility

**Performance Optimization:**
- Monitor endpoint performance
- Track resource utilization trends

**Operational Excellence:**
- 24/7 monitoring
- Historical data
- Complete transparency into infrastructure performance

## 🎯 ICF Application Optimizations

Based on your feedback about needing event indexing, week-long data retention, and heavy usage of `GetTxResponse`, `GetTxResponsesForEvent`, and `BankQueryBalance` endpoints, we've made specific optimizations to ensure optimal performance for your application.

### 1. **Event Indexing Support** ✅
**Your Need:** "We rely on event indexing as well"

**Our Optimization:**
```toml
# Targeted event indexing for applications
index-events = [
  "tx", "message.action", "message.sender", "message.module",
  "transfer.recipient", "transfer.sender", "transfer.amount",
  "coin_spent.spender", "coin_received.receiver", "coin_received.amount"
]
```

**Why:** We've specifically configured indexing for the most common type of events applications need most, focusing on transfers, message routing, and balance changes rather than indexing everything. These optimizations can be adjusted as needed - see [Future Optimization Opportunities](#-future-optimization-opportunities) below for additional customization options.

### 2. **Week-Long Data Retention** ✅
**Your Need:** "Would be great if we could retain a week of data"

**Our Optimization:**
```toml
# Custom pruning for 1-week retention (chain-specific)
pruning:
  strategy: custom
  keepRecent: 604800
  interval: 5000
  minRetainBlocks: 604800

# For slower chains (6s blocks):
pruning-keep-recent = 100800   # 1 week at 6s blocks = 100,800 blocks
pruning-interval = 1000        # Less frequent pruning needed
```

**Why:** We pre-calculated exact block retention by analyzing the last 1000 blocks for each chain to determine average block times. Fast chains (1s blocks) need 604,800 blocks for one week, while slower chains (6s blocks) only need 100,800 blocks. This ensures precise 7-day data retention regardless of chain speed.

### 3. **Optimized for Your Heavy-Use Endpoints** ✅
**Your Need:** "We use three endpoints heavily: GetTxResponse, GetTxResponsesForEvent, and BankQueryBalance"

**Our Optimizations:**

**For GetTxResponse (Transaction Lookups):**
```toml
# Faster transaction queries
inter-block-cache = true
iavl-cache-size = 3000000  # Optimized for 1s blocks
```

**For GetTxResponsesForEvent (Event-based Queries):**
```toml
# Faster event queries with proper indexing
query-gas-limit = 2000000000  # Handle complex event queries
# + Event indexing configuration above
```

**For BankQueryBalance (Balance Queries):**
```toml
# IAVL optimizations for state queries
iavl-disable-fastnode = false  # Keep fastnode for faster balance lookups
iavl-lazy-loading = false      # Immediate loading for faster queries
```

### 4. **Addressing Performance Issues** ✅
**Your Problem:** "We've had issues with lagging nodes and general downtime"

**Our Optimizations:**

**Mempool Optimization:**
```toml
[mempool]
max-txs = 15000
recheck = true
keep-invalid-txs-in-cache = false
size = 15000
max_txs_bytes = 1073741824
cache_size = 30000
```

**Network Performance:**
```toml
# Fast chains (1s blocks) - optimized for high throughput
send_rate = 5120000
recv_rate = 5120000
timeout_broadcast_tx_commit = "10s"

# Slower chains (6s blocks) - conservative settings
send_rate = 2048000
recv_rate = 2048000
timeout_broadcast_tx_commit = "60s"
```

**Connection Management:**
```toml
# Optimized for your current needs with room for growth
max_subscription_clients = 100
max_open_connections = 100
experimental_close_on_slow_client = true

# API optimizations
max-open-connections = 1500
rpc-read-timeout = 15
rpc-write-timeout = 15
```

## 🚀 Future Optimization Opportunities

### Range Query Enhancement

**Current Status:** Infrastructure uses optimized exact height queries achieving <50ms response times.

**For Advanced Use Cases:** If applications require range queries (`tx.height>X`), we can implement targeted optimizations. Several strategies available:

1. **IAVL Cache Tuning** - Immediate performance boost
2. **PostgreSQL Indexer** - Dramatic improvement for range queries
3. **Selective Event Indexing** - Reduced index size optimization
4. **Resource Scaling** - Enhanced compute if needed

## 🔄 Continuous Improvement

Please let us know if you notice any issues or want different tuning - we'll update ASAP. We're confident that our deep knowledge of the Cosmos SDK and ability to iterate fast will ensure you're always able to query the data you need on the node in a timely manner that's always available.

**Key Advantages:**
- 🎯 **Targeted optimizations** based on your specific endpoints
- ⚡ **Fast iteration** - changes deployed quickly
- 📈 **Performance monitoring** via our benchmark tool
- 🔧 **Resource efficient** - optimized for multi-chain environment
- 📊 **Measurable results** - clear performance metrics

## 🆘 Support & Contact

### Getting Help

If you encounter any issues or have questions:

1. **Telegram**: Contact Dan Bryan directly in our existing Telegram channel
2. **GitHub Issues**: Create an issue in this repository and tag `@danb` - I'll resolve it promptly

### Response Times

- **Telegram**: Usually within a few hours during business hours
- **GitHub Issues**: Typically resolved within 24 hours
- **Critical Issues**: Immediate response via Telegram

---

## 🧪 Benchmark Tool - Validation & Testing

As part of your delivery, we've included a custom benchmark tool that validates the performance optimizations and confirms your infrastructure is operating at peak efficiency.

### What the Benchmark Tests

Based on your application requirements:

1. **Data Retention** - Confirms 1+ week of historical blockchain data availability
2. **GetTxResponse** - Individual transaction lookup latency and functionality
3. **GetTxResponsesForEvent** - Event-based query performance + indexing verification
4. **BankQueryBalance** - Account balance query response times
5. **Event Indexing** - Validates application-relevant events are properly indexed
6. **Protocol Coverage** - Tests RPC, REST API, and GRPC endpoints

### Using the Benchmark Tool

**Prerequisites:**
- **Go 1.23+** - [Install Go](https://golang.org/doc/install)
- **Git** - For cloning the repository

**Installation & Usage:**

```bash
# Clone the repository
git clone https://github.com/bryanlabs/icf-nodes
cd icf-nodes

# Build both tools
make build

# Test individual chains with benchmark tool
./benchmark neutron
./benchmark dydx
./benchmark pryzm

# Test all enabled chains
./benchmark all

# Or run directly with Go
go run ./cmd/benchmark all

# Run stress tests
./stress neutron 100 60  # 100 workers for 60 seconds
go run ./cmd/stress pryzm 50 30  # Or run directly
```

**Configuration:**

The benchmark tool uses the included `config.yaml` file which defines:
- **Chain endpoints** (RPC, API, GRPC, WebSocket URLs)
- **Block times** (pre-calculated from chain analysis)
- **Performance thresholds** (pass/warn/fail criteria)
- **Chain-specific optimizations** (SDK versions, query styles)

### Development with VS Code

This repository includes two separate testing tools:
- **cmd/benchmark/** - Performance benchmarking tool
- **cmd/stress/** - Aggressive load testing tool

The tools are organized in separate directories to avoid naming conflicts:

```bash
# Build both tools
make build

# Run the benchmark tool
make benchmark ARGS=neutron
# or directly:
go run ./cmd/benchmark neutron
./benchmark neutron  # If already built

# Run the stress test tool  
make stress ARGS='neutron 100 60'
# or directly:
go run ./cmd/stress neutron 100 60
./stress neutron 100 60  # If already built
```

**Project Structure:**
```
icf-nodes/
├── cmd/
│   ├── benchmark/
│   │   └── main.go    # Benchmark tool
│   └── stress/
│       └── main.go    # Stress test tool
├── config.yaml        # Benchmark configuration
├── stress-config.yaml # Stress test configuration
└── Makefile          # Build automation
```

---
