// Package main provides a comprehensive benchmark tool for Cosmos SDK blockchain nodes.
//
// This tool validates node configuration and performance across all protocols (RPC, REST API, GRPC, WebSocket)
// with concurrent execution, proper error handling, and detailed reporting.
//
// Features:
//   - Concurrent benchmark execution for optimal performance
//   - Context-aware HTTP requests with proper timeouts
//   - Thread-safe result collection and reporting
//   - Configuration-driven chain support
//
// Usage:
//
//	go run benchmark.go <chain>     # Test specific chain
//	go run benchmark.go all         # Test all enabled chains
//	go run benchmark.go --json      # Output JSON format
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"

	// Cosmos SDK GRPC services
	"github.com/cosmos/cosmos-sdk/types/query"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// Config represents the entire configuration structure loaded from config.yaml.
// It defines chains to test, performance thresholds, and execution parameters.
type Config struct {
	Chains     map[string]ChainConfig `yaml:"chains"`
	Thresholds ThresholdConfig        `yaml:"thresholds"`
	TestConfig TestConfig             `yaml:"test_config"`
}

// ChainConfig represents configuration for a single blockchain chain.
// It includes endpoints, timing parameters, and chain-specific settings.
type ChainConfig struct {
	Name            string                `yaml:"name"`
	RPC             string                `yaml:"rpc"`
	API             string                `yaml:"api"`
	GRPC            string                `yaml:"grpc"`
	WebSocket       string                `yaml:"websocket"`
	BlockTime       time.Duration         `yaml:"block_time"`
	Enabled         bool                  `yaml:"enabled"`
	SDKVersion      string                `yaml:"sdk_version"`
	ConsumerChain   bool                  `yaml:"consumer_chain"`
	APIQueryStyle   string                `yaml:"api_query_style"`
	TestAddresses   []string              `yaml:"test_addresses"`
	QueryStrategies QueryStrategiesConfig `yaml:"query_strategies"`
}

// QueryStrategiesConfig represents query strategy configuration for a chain
type QueryStrategiesConfig struct {
	TxHeightQuery       string `yaml:"tx_height_query"`       // "exact" or "range"
	FallbackQueryHeight int64  `yaml:"fallback_query_height"` // specific height to use for testing
}

// ThresholdConfig represents performance thresholds
type ThresholdConfig struct {
	DataRetention          RetentionThreshold  `yaml:"data_retention"`
	GetTxResponse          LatencyThreshold    `yaml:"get_tx_response"`
	GetTxResponsesForEvent LatencyThreshold    `yaml:"get_tx_responses_for_event"`
	BankQueryBalance       LatencyThreshold    `yaml:"bank_query_balance"`
	EventIndexing          EventIndexThreshold `yaml:"event_indexing"`
}

// RetentionThreshold represents data retention thresholds
type RetentionThreshold struct {
	TargetDays int `yaml:"target_days"`
	WarnDays   int `yaml:"warn_days"`
	FailDays   int `yaml:"fail_days"`
}

// LatencyThreshold represents latency-based thresholds
type LatencyThreshold struct {
	Pass time.Duration `yaml:"pass"`
	Warn time.Duration `yaml:"warn"`
	Fail time.Duration `yaml:"fail"`
}

// EventIndexThreshold represents event indexing thresholds
type EventIndexThreshold struct {
	RequiredEvents int `yaml:"required_events"`
	PassThreshold  int `yaml:"pass_threshold"`
	WarnThreshold  int `yaml:"warn_threshold"`
}

// TestConfig represents test execution configuration
type TestConfig struct {
	Timeout           time.Duration `yaml:"timeout"`
	RetryAttempts     int           `yaml:"retry_attempts"`
	ParallelExecution bool          `yaml:"parallel_execution"`
	BlockSearchDepth  int           `yaml:"block_search_depth"` // How many blocks to search for transactions
	TestIterations    int           `yaml:"test_iterations"`    // Number of iterations to run for each test
}

// ChainEndpoints defines the endpoint configuration for a blockchain
type ChainEndpoints struct {
	Name      string
	RPC       string
	API       string
	GRPC      string
	WebSocket string
}

// BlockWithTransactions contains discovered transaction data for testing
type BlockWithTransactions struct {
	Height          int64
	TransactionHash string
	TestAddress     string
	HasTransactions bool
}

// BenchmarkResult represents the outcome of a single benchmark test.
// It includes timing, status, and contextual information for analysis.
type BenchmarkResult struct {
	Chain     string        `json:"chain"`
	Test      string        `json:"test"`
	Status    string        `json:"status"`
	Latency   time.Duration `json:"latency,omitempty"`
	Message   string        `json:"message"`
	Details   interface{}   `json:"details,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

// Global configuration loaded from config.yaml
var config Config

// Results storage
var (
	results   []BenchmarkResult
	resultsMu sync.Mutex
)

// loadConfig loads configuration from config.yaml file
func loadConfig() error {
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		return fmt.Errorf("failed to read config.yaml: %v", err)
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config.yaml: %v", err)
	}

	// Validate the loaded configuration
	if err := validateConfig(); err != nil {
		return fmt.Errorf("invalid config.yaml: %v", err)
	}

	return nil
}

// getChainEndpoints converts ChainConfig to ChainEndpoints for compatibility
func getChainEndpoints(chainKey string) (ChainEndpoints, bool) {
	chainConfig, exists := config.Chains[chainKey]
	if !exists || !chainConfig.Enabled {
		return ChainEndpoints{}, false
	}

	return ChainEndpoints{
		Name:      chainConfig.Name,
		RPC:       chainConfig.RPC,
		API:       chainConfig.API,
		GRPC:      chainConfig.GRPC,
		WebSocket: chainConfig.WebSocket,
	}, true
}

// getAvailableChains returns list of enabled chain names
func getAvailableChains() []string {
	var chains []string
	for key, chainConfig := range config.Chains {
		if chainConfig.Enabled {
			chains = append(chains, key)
		}
	}
	return chains
}

// getThresholdStatus returns status based on latency and configured thresholds
func getThresholdStatus(latency time.Duration, threshold LatencyThreshold) string {
	if latency <= threshold.Pass {
		return "PASS"
	} else if latency <= threshold.Warn {
		return "WARN"
	} else {
		return "FAIL"
	}
}

// runMultipleIterations runs a test function multiple times and logs aggregated results
func runMultipleIterations(testFunc func() (time.Duration, string, string, map[string]interface{}),
	chainName, testName string, iterations int) {

	if iterations <= 0 {
		iterations = 1
	}

	latencies := make([]time.Duration, 0, iterations)
	var lastStatus string
	var lastMessage string
	var lastMetadata map[string]interface{}
	failCount := 0

	// Run multiple iterations
	for i := 0; i < iterations; i++ {
		latency, status, message, metadata := testFunc()

		if status != "FAIL" {
			latencies = append(latencies, latency)
		} else {
			failCount++
		}

		lastStatus = status
		lastMessage = message
		lastMetadata = metadata
	}

	// If all tests failed, log the failure
	if len(latencies) == 0 {
		logResult(chainName, testName, lastStatus, lastMessage, 0, lastMetadata)
		return
	}

	// Calculate statistics and determine overall status
	stats := calculateLatencyStats(latencies)
	overallStatus := lastStatus

	// If some tests failed but not all, mark as WARN
	if failCount > 0 && failCount < iterations {
		overallStatus = "WARN"
		lastMessage = fmt.Sprintf("%s (%d/%d successful)", lastMessage, len(latencies), iterations)
	}

	// Log the aggregated result with statistics
	if lastMetadata == nil {
		lastMetadata = make(map[string]interface{})
	}
	lastMetadata["iterations"] = iterations
	lastMetadata["successful_tests"] = len(latencies)
	lastMetadata["failed_tests"] = failCount
	lastMetadata["stats"] = stats
	lastMetadata["show_iterations"] = iterations // Used by reporting to show actual iteration count

	logResult(chainName, testName, overallStatus, lastMessage, stats.Mean, lastMetadata)
}

// runMultipleIterationsSimple runs a test function multiple times and aggregates results
func runMultipleIterationsSimple(testFunc func(), chainName, testName string, iterations int) {
	if iterations <= 0 {
		iterations = 1
	}

	// Clear any existing results for this test to avoid duplicates
	resultsMu.Lock()
	filteredResults := make([]BenchmarkResult, 0)
	for _, r := range results {
		if !(r.Chain == chainName && r.Test == testName) {
			filteredResults = append(filteredResults, r)
		}
	}
	results = filteredResults
	resultsMu.Unlock()

	// Run multiple iterations
	var latencies []time.Duration

	for i := 0; i < iterations; i++ {
		// Add delay between iterations to prevent connection exhaustion and race conditions
		if i > 0 {
			time.Sleep(1 * time.Millisecond)
		}

		// Get a snapshot of current results to find our specific result after test
		resultsMu.Lock()
		beforeResults := make([]BenchmarkResult, len(results))
		copy(beforeResults, results)
		beforeCount := len(results)
		resultsMu.Unlock()

		// Run the test
		testFunc()

		// Find the new result added by THIS specific test (matching chain and test name)
		// This prevents race conditions where other concurrent tests add results
		resultsMu.Lock()
		foundNewResult := false
		for j := beforeCount; j < len(results); j++ {
			if results[j].Chain == chainName && results[j].Test == testName {
				newResult := results[j]
				if newResult.Status != "FAIL" && newResult.Latency > 0 {
					latencies = append(latencies, newResult.Latency)
				}
				foundNewResult = true
				// Don't break - collect all results from iterations
			}
		}

		// Fallback: if no exact match found but results were added, check if it's our chain
		// This handles edge cases where test name might vary slightly
		if !foundNewResult && len(results) > beforeCount {
			// Look for any result from our chain that was just added
			for j := beforeCount; j < len(results); j++ {
				if results[j].Chain == chainName {
					newResult := results[j]
					if newResult.Status != "FAIL" && newResult.Latency > 0 {
						latencies = append(latencies, newResult.Latency)
					}
					// Don't break - collect all results from iterations
				}
			}
		}
		resultsMu.Unlock()
	}

	// If we have multiple results, add metadata to individual results instead of aggregating
	if iterations > 1 {
		// Add metadata to all individual results for this test to show they're part of multiple iterations
		resultsMu.Lock()
		for i := range results {
			if results[i].Chain == chainName && results[i].Test == testName {
				if results[i].Details == nil {
					results[i].Details = make(map[string]interface{})
				}
				if detailsMap, ok := results[i].Details.(map[string]interface{}); ok {
					detailsMap["show_iterations"] = iterations
					detailsMap["successful_tests"] = len(latencies)
					detailsMap["failed_tests"] = iterations - len(latencies)
				}
			}
		}
		resultsMu.Unlock()

	}
}

// getDataRetentionStatus returns status based on retention days and configured thresholds
func getDataRetentionStatus(retentionDays float64) string {
	retention := config.Thresholds.DataRetention
	if retentionDays >= float64(retention.TargetDays) {
		return "PASS"
	} else if retentionDays >= float64(retention.WarnDays) {
		return "WARN"
	} else {
		return "FAIL"
	}
}

// validateChainConfig validates that a chain configuration has all required fields
func validateChainConfig(chainKey string, chainConfig ChainConfig) error {
	if chainConfig.Name == "" {
		return fmt.Errorf("chain %s: name is required", chainKey)
	}
	if chainConfig.RPC == "" {
		return fmt.Errorf("chain %s: rpc endpoint is required", chainKey)
	}
	if chainConfig.API == "" {
		return fmt.Errorf("chain %s: api endpoint is required", chainKey)
	}
	if chainConfig.GRPC == "" {
		return fmt.Errorf("chain %s: grpc endpoint is required", chainKey)
	}
	if chainConfig.WebSocket == "" {
		return fmt.Errorf("chain %s: websocket endpoint is required", chainKey)
	}
	if chainConfig.BlockTime <= 0 {
		return fmt.Errorf("chain %s: block_time must be positive", chainKey)
	}
	if chainConfig.SDKVersion == "" {
		return fmt.Errorf("chain %s: sdk_version is required", chainKey)
	}
	if chainConfig.APIQueryStyle != "query" && chainConfig.APIQueryStyle != "events" {
		return fmt.Errorf("chain %s: api_query_style must be 'query' or 'events'", chainKey)
	}
	return nil
}

// validateConfig validates the entire configuration
func validateConfig() error {
	for chainKey, chainConfig := range config.Chains {
		if chainConfig.Enabled {
			if err := validateChainConfig(chainKey, chainConfig); err != nil {
				return err
			}
		}
	}
	return nil
}

func logResult(chain, test, status, message string, latency time.Duration, details interface{}) {
	result := BenchmarkResult{
		Chain:     chain,
		Test:      test,
		Status:    status,
		Message:   message,
		Latency:   latency,
		Details:   details,
		Timestamp: time.Now(),
	}

	resultsMu.Lock()
	results = append(results, result)
	resultsMu.Unlock()

	// Real-time logging disabled to avoid duplicate output - results shown in organized report
}

var (
	httpClientOnce   sync.Once
	sharedHTTPClient *http.Client
)

// getHTTPClient returns a singleton HTTP client with optimal connection pooling
func getHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		sharedHTTPClient = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        200, // Increased for better connection reuse
				MaxIdleConnsPerHost: 20,  // Increased per host
				IdleConnTimeout:     120 * time.Second,
				DisableKeepAlives:   false,
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: false, // Proper TLS validation
					MinVersion:         tls.VersionTLS12,
				},
				// Additional optimizations
				MaxConnsPerHost:       50,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		}
	})
	return sharedHTTPClient
}

// httpGetWithContext performs HTTP GET with context and proper error handling
func httpGetWithContext(ctx context.Context, url string) (*http.Response, error) {
	client := getHTTPClient()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for %s: %w", url, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request for %s: %w", url, err)
	}

	return resp, nil
}

// httpGetWithRetry performs HTTP GET with exponential backoff retry logic
func httpGetWithRetry(ctx context.Context, url string, maxRetries int) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, 8s... (capped to prevent overflow)
			safeAttempt := attempt - 1
			if safeAttempt > 10 {
				safeAttempt = 10 // Cap to prevent integer overflow
			}
			// Safe conversion with explicit bounds checking
			if safeAttempt < 0 {
				safeAttempt = 0
			}
			backoffDuration := time.Duration(1<<safeAttempt) * time.Second
			select {
			case <-time.After(backoffDuration):
				// Continue with retry
			case <-ctx.Done():
				return nil, fmt.Errorf("context cancelled during retry backoff: %w", ctx.Err())
			}
		}

		resp, err := httpGetWithContext(ctx, url)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		// Don't retry on certain error types
		if isNonRetryableError(err) {
			break
		}
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", maxRetries+1, lastErr)
}

// isNonRetryableError determines if an error should not be retried
func isNonRetryableError(err error) bool {
	// Don't retry on context cancellation or certain HTTP status codes
	if err == context.Canceled || err == context.DeadlineExceeded {
		return true
	}

	errStr := err.Error()
	// Don't retry on 4xx client errors (except 429 Too Many Requests)
	return strings.Contains(errStr, "400") || strings.Contains(errStr, "401") ||
		strings.Contains(errStr, "403") || strings.Contains(errStr, "404")
}

// LatencyStats represents statistical analysis of latencies
type LatencyStats struct {
	Count   int           `json:"count"`
	Min     time.Duration `json:"min"`
	Max     time.Duration `json:"max"`
	Mean    time.Duration `json:"mean"`
	P50     time.Duration `json:"p50"`
	P95     time.Duration `json:"p95"`
	P99     time.Duration `json:"p99"`
	TotalMs int64         `json:"total_ms"`
}

// calculateLatencyStats computes statistical metrics for a set of latencies
func calculateLatencyStats(latencies []time.Duration) LatencyStats {
	if len(latencies) == 0 {
		return LatencyStats{}
	}

	// Sort latencies for percentile calculations
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	// Calculate basic stats
	stats := LatencyStats{
		Count: len(sorted),
		Min:   sorted[0],
		Max:   sorted[len(sorted)-1],
	}

	// Calculate mean
	var total time.Duration
	for _, lat := range sorted {
		total += lat
	}
	stats.Mean = total / time.Duration(len(sorted))
	stats.TotalMs = total.Nanoseconds() / 1000000

	// Calculate percentiles
	stats.P50 = calculatePercentile(sorted, 50)
	stats.P95 = calculatePercentile(sorted, 95)
	stats.P99 = calculatePercentile(sorted, 99)

	return stats
}

// calculatePercentile calculates the nth percentile from sorted latencies
func calculatePercentile(sorted []time.Duration, percentile int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	index := float64(percentile) / 100.0 * float64(len(sorted)-1)

	if index == math.Trunc(index) {
		return sorted[int(index)]
	}

	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))

	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}

	weight := index - math.Floor(index)
	return time.Duration(float64(sorted[lower].Nanoseconds())*(1-weight) +
		float64(sorted[upper].Nanoseconds())*weight)
}

// checkDataRetention verifies how much historical data is available
func checkDataRetention(chainEndpoints ChainEndpoints) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Get current block height with retry logic
	maxRetries := config.TestConfig.RetryAttempts
	if maxRetries <= 0 {
		maxRetries = 2 // Default fallback
	}
	resp, err := httpGetWithRetry(ctx, chainEndpoints.RPC+"/status", maxRetries)
	if err != nil {
		logResult(chainEndpoints.Name, "Data Retention", "FAIL", fmt.Sprintf("Failed to get current status: %v", err), time.Since(start), nil)
		return
	}
	defer resp.Body.Close()

	var statusResp struct {
		Result struct {
			SyncInfo struct {
				LatestBlockHeight string `json:"latest_block_height"`
				LatestBlockTime   string `json:"latest_block_time"`
			} `json:"sync_info"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		logResult(chainEndpoints.Name, "Data Retention", "FAIL", fmt.Sprintf("Failed to parse status: %v", err), time.Since(start), nil)
		return
	}

	currentHeight, err := strconv.ParseInt(statusResp.Result.SyncInfo.LatestBlockHeight, 10, 64)
	if err != nil {
		logResult(chainEndpoints.Name, "Data Retention", "FAIL", fmt.Sprintf("Failed to parse block height '%s': %v", statusResp.Result.SyncInfo.LatestBlockHeight, err), time.Since(start), nil)
		return
	}

	// Get block time from config
	var blockTime time.Duration
	chainKey := strings.ToLower(chainEndpoints.Name)
	if chainConfig, exists := config.Chains[chainKey]; exists {
		blockTime = chainConfig.BlockTime
	} else {
		// This should not happen since we validate config loading
		logResult(chainEndpoints.Name, "Data Retention", "FAIL", "Chain not found in config", time.Since(start), nil)
		return
	}

	estimatedBlocksInWeek := int64(7 * 24 * 3600 / blockTime.Seconds())
	estimatedOldHeight := currentHeight - estimatedBlocksInWeek

	if estimatedOldHeight < 1 {
		estimatedOldHeight = 1
	}

	// Try to fetch a block from a week ago
	blockURL := fmt.Sprintf("%s/block?height=%d", chainEndpoints.RPC, estimatedOldHeight)
	reqCtx, cancel := context.WithTimeout(context.Background(), config.TestConfig.Timeout)
	defer cancel()
	blockResp, err := httpGetWithRetry(reqCtx, blockURL, config.TestConfig.RetryAttempts)
	latency := time.Since(start)

	if err != nil {
		logResult(chainEndpoints.Name, "Data Retention", "FAIL", fmt.Sprintf("Failed to fetch historical block: %v", err), latency, nil)
		return
	}
	defer blockResp.Body.Close()

	if blockResp.StatusCode != 200 {
		// Try to find the earliest available block
		earliestHeight := findEarliestBlock(chainEndpoints, currentHeight)
		if earliestHeight > 0 {
			actualRetention := calculateRetention(currentHeight, earliestHeight, blockTime)
			retentionDays := actualRetention.Hours() / 24
			status := getDataRetentionStatus(retentionDays)
			targetDays := config.Thresholds.DataRetention.TargetDays

			if status == "FAIL" {
				logResult(chainEndpoints.Name, "Data Retention", status,
					fmt.Sprintf("Only %.1f days of data available (target: %d+ days)", retentionDays, targetDays),
					latency, map[string]interface{}{
						"retention_days":  retentionDays,
						"earliest_height": earliestHeight,
						"current_height":  currentHeight,
					})
			} else {
				logResult(chainEndpoints.Name, "Data Retention", status,
					fmt.Sprintf("Has %.1f days of data (target: %d+ days)", retentionDays, targetDays),
					latency, map[string]interface{}{
						"retention_days":  retentionDays,
						"earliest_height": earliestHeight,
						"current_height":  currentHeight,
					})
			}
		} else {
			logResult(chainEndpoints.Name, "Data Retention", "FAIL", "Could not determine data retention", latency, nil)
		}
		return
	}

	logResult(chainEndpoints.Name, "Data Retention", "PASS", "Full week of historical data available", latency,
		map[string]interface{}{
			"retention_days": 7.0,
			"test_height":    estimatedOldHeight,
			"current_height": currentHeight,
		})
}

// findEarliestBlock uses binary search to find the earliest available block
func findEarliestBlock(chainEndpoints ChainEndpoints, currentHeight int64) int64 {
	low := int64(1)
	high := currentHeight
	var earliest int64 = -1

	for i := 0; i < 20 && low <= high; i++ { // Limit iterations
		mid := (low + high) / 2
		blockURL := fmt.Sprintf("%s/block?height=%d", chainEndpoints.RPC, mid)

		reqCtx, cancel := context.WithTimeout(context.Background(), config.TestConfig.Timeout)
		defer cancel()
		resp, err := httpGetWithRetry(reqCtx, blockURL, config.TestConfig.RetryAttempts)
		if err != nil {
			break
		}
		if closeErr := resp.Body.Close(); closeErr != nil {
			// Log but don't fail on close error
		}

		if resp.StatusCode == 200 {
			earliest = mid
			high = mid - 1
		} else {
			low = mid + 1
		}
	}

	return earliest
}

func calculateRetention(currentHeight, earliestHeight int64, blockTime time.Duration) time.Duration {
	blocks := currentHeight - earliestHeight
	return time.Duration(blocks) * blockTime
}

// getCurrentHeight gets the current block height for the chain
func getCurrentHeight(chainEndpoints ChainEndpoints) (int64, error) {
	reqCtx, cancel := context.WithTimeout(context.Background(), config.TestConfig.Timeout)
	defer cancel()
	resp, err := httpGetWithRetry(reqCtx, chainEndpoints.RPC+"/status", config.TestConfig.RetryAttempts)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var statusData struct {
		Result struct {
			SyncInfo struct {
				LatestBlockHeight string `json:"latest_block_height"`
			} `json:"sync_info"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&statusData); err != nil {
		return 0, err
	}

	return strconv.ParseInt(statusData.Result.SyncInfo.LatestBlockHeight, 10, 64)
}

// findBlockWithTransactions discovers a block with actual transactions for testing
func findBlockWithTransactions(chainEndpoints ChainEndpoints) *BlockWithTransactions {
	// Block Discovery is a setup test, run once not multiple iterations
	return findBlockWithTransactionsSingle(chainEndpoints)
}

// findBlockWithTransactionsSingle performs the actual block discovery logic
func findBlockWithTransactionsSingle(chainEndpoints ChainEndpoints) *BlockWithTransactions {
	currentHeight, err := getCurrentHeight(chainEndpoints)
	if err != nil {
		logResult(chainEndpoints.Name, "Block Discovery", "FAIL", fmt.Sprintf("Failed to get current height: %v", err), 0, nil)
		return nil
	}

	// Search backwards from current height to find a block with transactions
	searchDepth := int64(config.TestConfig.BlockSearchDepth)
	if searchDepth <= 0 {
		searchDepth = 50 // Default fallback
	}
	for offset := int64(1); offset <= searchDepth; offset++ {
		testHeight := currentHeight - offset
		if testHeight < 1 {
			break
		}

		// Try RPC first to get block data
		blockURL := fmt.Sprintf("%s/block?height=%d", chainEndpoints.RPC, testHeight)
		reqCtx, cancel := context.WithTimeout(context.Background(), config.TestConfig.Timeout)
		defer cancel()
		resp, err := httpGetWithRetry(reqCtx, blockURL, config.TestConfig.RetryAttempts)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			continue
		}

		var blockResp struct {
			Result struct {
				Block struct {
					Data struct {
						Txs []string `json:"txs"`
					} `json:"data"`
				} `json:"block"`
			} `json:"result"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&blockResp); err != nil {
			continue
		}

		// Check if block has transactions
		if len(blockResp.Result.Block.Data.Txs) > 0 {
			// Try to get transaction details via RPC tx_search
			searchURL := fmt.Sprintf("%s/tx_search?query=\"tx.height=%d\"&per_page=5", chainEndpoints.RPC, testHeight)
			searchCtx, searchCancel := context.WithTimeout(context.Background(), config.TestConfig.Timeout)
			defer searchCancel()
			searchResp, err := httpGetWithRetry(searchCtx, searchURL, config.TestConfig.RetryAttempts)
			if err != nil {
				continue
			}
			defer searchResp.Body.Close()

			if searchResp.StatusCode != 200 {
				continue
			}

			var txSearchResp struct {
				Result struct {
					Txs []struct {
						Hash     string `json:"hash"`
						TxResult struct {
							Code   int `json:"code"`
							Events []struct {
								Type       string `json:"type"`
								Attributes []struct {
									Key   string `json:"key"`
									Value string `json:"value"`
								} `json:"attributes"`
							} `json:"events"`
						} `json:"tx_result"`
					} `json:"txs"`
				} `json:"result"`
			}

			if err := json.NewDecoder(searchResp.Body).Decode(&txSearchResp); err != nil {
				continue
			}

			// Find a successful transaction and extract an address
			for _, tx := range txSearchResp.Result.Txs {
				if tx.TxResult.Code == 0 { // Successful transaction
					var testAddress string

					// Look for sender/recipient addresses in events
					for _, event := range tx.TxResult.Events {
						for _, attr := range event.Attributes {
							if (attr.Key == "sender" || attr.Key == "recipient") &&
								(strings.Contains(attr.Value, chainEndpoints.Name[:3]) ||
									strings.Contains(attr.Value, "1")) {
								testAddress = attr.Value
								break
							}
						}
						if testAddress != "" {
							break
						}
					}

					logResult(chainEndpoints.Name, "Block Discovery", "PASS",
						fmt.Sprintf("Found block %d with %d transactions", testHeight, len(blockResp.Result.Block.Data.Txs)),
						0, map[string]interface{}{
							"block_height": testHeight,
							"tx_count":     len(blockResp.Result.Block.Data.Txs),
							"test_tx_hash": tx.Hash,
							"test_address": testAddress,
						})

					return &BlockWithTransactions{
						Height:          testHeight,
						TransactionHash: tx.Hash,
						TestAddress:     testAddress,
						HasTransactions: true,
					}
				}
			}
		}
	}

	// Fallback: return current height with default addresses
	logResult(chainEndpoints.Name, "Block Discovery", "WARN",
		"No blocks with transactions found, using current height with default address",
		0, map[string]interface{}{
			"fallback_height": currentHeight,
		})

	return &BlockWithTransactions{
		Height:          currentHeight,
		TransactionHash: "",
		TestAddress:     getTestAddress(chainEndpoints),
		HasTransactions: false,
	}
}

// getGRPCConnection creates a GRPC connection to the chain
func getGRPCConnection(endpoint string) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption

	if strings.Contains(endpoint, ":443") {
		// Use TLS for port 443
		creds := credentials.NewTLS(&tls.Config{
			MinVersion: tls.VersionTLS12,
		})
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		// Use insecure for other ports
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(endpoint, opts...)
	if err != nil {
		return nil, err
	}

	// Force connection establishment with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to establish the connection by waiting for it to be ready
	conn.Connect()
	for {
		state := conn.GetState()
		if state.String() == "READY" {
			break
		}
		if state.String() == "TRANSIENT_FAILURE" || state.String() == "SHUTDOWN" {
			if closeErr := conn.Close(); closeErr != nil {
				// Log but don't fail on close error
			}
			return nil, fmt.Errorf("failed to connect to GRPC endpoint: %s", state.String())
		}
		if !conn.WaitForStateChange(ctx, state) {
			// Timeout occurred
			if closeErr := conn.Close(); closeErr != nil {
				// Log but don't fail on close error
			}
			return nil, fmt.Errorf("timeout waiting for GRPC connection to be ready")
		}
	}

	return conn, nil
}

// checkGetTxResponseGRPC tests transaction queries via GRPC using actual Cosmos SDK service calls
func checkGetTxResponseGRPC(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {

	// Run multiple iterations for statistical analysis
	iterations := config.TestConfig.TestIterations
	if iterations <= 0 {
		iterations = 1
	}

	testFunc := func() (time.Duration, string, string, map[string]interface{}) {
		return runSingleGetTxResponseGRPC(chainEndpoints, blockData)
	}

	runMultipleIterations(testFunc, chainEndpoints.Name, "GetTxResponse-GRPC", iterations)
}

// runSingleGetTxResponseGRPC runs a single GRPC GetTxResponse test
func runSingleGetTxResponseGRPC(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) (time.Duration, string, string, map[string]interface{}) {
	start := time.Now()

	if blockData.TransactionHash == "" {
		return time.Since(start), "FAIL", "No transaction hash available for GRPC test", nil
	}

	conn, err := getGRPCConnection(chainEndpoints.GRPC)
	if err != nil {
		return time.Since(start), "WARN", fmt.Sprintf("GRPC endpoint not accessible: %v", err),
			map[string]interface{}{
				"endpoint_type": "GRPC",
				"note":          "GRPC endpoint may not be exposed or configured",
			}
	}
	defer conn.Close()

	// Create Tx service client and perform actual GetTx call
	txClient := txtypes.NewServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req := &txtypes.GetTxRequest{
		Hash: blockData.TransactionHash,
	}

	resp, err := txClient.GetTx(ctx, req)
	latency := time.Since(start)

	if err != nil {
		// Handle common GRPC errors gracefully
		errStr := err.Error()
		if strings.Contains(errStr, "Unimplemented") {
			return latency, "WARN", "GetTx service not implemented on this endpoint",
				map[string]interface{}{
					"endpoint_type": "GRPC",
					"error_type":    "service_unavailable",
					"note":          "Chain may not expose Tx service via GRPC",
				}
		} else if strings.Contains(errStr, "NotFound") {
			return latency, "WARN", "Transaction not found via GRPC service",
				map[string]interface{}{
					"endpoint_type": "GRPC",
					"test_tx_hash":  blockData.TransactionHash,
					"error_type":    "not_found",
					"note":          "Transaction may not be indexed for GRPC queries",
				}
		} else {
			return latency, "FAIL", fmt.Sprintf("GetTx GRPC call failed: %v", err),
				map[string]interface{}{
					"endpoint_type": "GRPC",
					"test_tx_hash":  blockData.TransactionHash,
					"error_type":    "call_failed",
				}
		}
	}

	if resp == nil {
		return latency, "FAIL", "GetTx returned nil response",
			map[string]interface{}{
				"endpoint_type": "GRPC",
				"test_tx_hash":  blockData.TransactionHash,
			}
	}

	// Check if we have TxResponse (the more common field in newer versions)
	if resp.TxResponse == nil {
		return latency, "FAIL", "GetTx returned response without TxResponse field",
			map[string]interface{}{
				"endpoint_type": "GRPC",
				"test_tx_hash":  blockData.TransactionHash,
				"resp_tx":       resp.Tx != nil,
			}
	}

	// Successful GRPC transaction query
	status := getThresholdStatus(latency, config.Thresholds.GetTxResponse)

	return latency, status, "DEBUG-TX-SUCCESS: Transaction query successful via GRPC service",
		map[string]interface{}{
			"endpoint_type": "GRPC",
			"test_tx_hash":  blockData.TransactionHash,
			"tx_height":     resp.TxResponse.Height,
			"tx_code":       resp.TxResponse.Code,
			"test_block":    blockData.Height,
		}
}

// checkGetTxResponseRPC tests transaction queries via RPC
func checkGetTxResponseRPC(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	// Run multiple iterations for statistical analysis
	iterations := config.TestConfig.TestIterations
	if iterations <= 0 {
		iterations = 1
	}

	runMultipleIterationsSimple(
		func() { checkGetTxResponseRPCSingle(chainEndpoints, blockData) },
		chainEndpoints.Name, "GetTxResponse-RPC", iterations)
}

// checkGetTxResponseRPCSingle runs a single RPC GetTxResponse test
func checkGetTxResponseRPCSingle(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	start := time.Now()

	// Use discovered transaction hash if available
	testTxHash := blockData.TransactionHash
	if testTxHash == "" {
		logResult(chainEndpoints.Name, "GetTxResponse-RPC", "FAIL", "No transaction hash available for RPC test", time.Since(start), nil)
		return
	}

	// Test individual transaction lookup
	txURL := fmt.Sprintf("%s/tx?hash=0x%s", chainEndpoints.RPC, testTxHash)
	reqCtx, cancel := context.WithTimeout(context.Background(), config.TestConfig.Timeout)
	defer cancel()
	resp, err := httpGetWithRetry(reqCtx, txURL, config.TestConfig.RetryAttempts)
	latency := time.Since(start)

	if err != nil {
		logResult(chainEndpoints.Name, "GetTxResponse-RPC", "FAIL", fmt.Sprintf("Transaction lookup failed: %v", err), latency, nil)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		logResult(chainEndpoints.Name, "GetTxResponse-RPC", "FAIL", fmt.Sprintf("Transaction lookup HTTP %d", resp.StatusCode), latency, nil)
		return
	}

	status := getThresholdStatus(latency, config.Thresholds.GetTxResponse)

	logResult(chainEndpoints.Name, "GetTxResponse-RPC", status, "Transaction query successful via RPC", latency,
		map[string]interface{}{
			"test_tx_hash":  testTxHash,
			"endpoint_type": "RPC",
			"test_block":    blockData.Height,
		})
}

// checkGetTxResponseAPI tests transaction queries via REST API
func checkGetTxResponseAPI(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	// Run multiple iterations for statistical analysis
	iterations := config.TestConfig.TestIterations
	if iterations <= 0 {
		iterations = 1
	}

	runMultipleIterationsSimple(
		func() { checkGetTxResponseAPISingle(chainEndpoints, blockData) },
		chainEndpoints.Name, "GetTxResponse-API", iterations)
}

// checkGetTxResponseAPISingle runs a single API GetTxResponse test
func checkGetTxResponseAPISingle(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	start := time.Now()

	// Use discovered transaction hash if available
	testTxHash := blockData.TransactionHash
	if testTxHash == "" {
		logResult(chainEndpoints.Name, "GetTxResponse-API", "FAIL", "No transaction hash available for API test", time.Since(start), nil)
		return
	}

	// Test individual transaction lookup
	txURL := fmt.Sprintf("%s/cosmos/tx/v1beta1/txs/%s", chainEndpoints.API, testTxHash)
	reqCtx, cancel := context.WithTimeout(context.Background(), config.TestConfig.Timeout)
	defer cancel()
	resp, err := httpGetWithRetry(reqCtx, txURL, config.TestConfig.RetryAttempts)
	latency := time.Since(start)

	if err != nil {
		logResult(chainEndpoints.Name, "GetTxResponse-API", "FAIL", fmt.Sprintf("Transaction lookup failed: %v", err), latency, nil)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		logResult(chainEndpoints.Name, "GetTxResponse-API", "FAIL", fmt.Sprintf("Transaction lookup HTTP %d", resp.StatusCode), latency, nil)
		return
	}

	status := getThresholdStatus(latency, config.Thresholds.GetTxResponse)

	logResult(chainEndpoints.Name, "GetTxResponse-API", status, "Transaction query successful via REST API", latency,
		map[string]interface{}{
			"test_tx_hash":  testTxHash,
			"endpoint_type": "REST API",
			"test_block":    blockData.Height,
		})
}

// checkGetTxResponse tests the GetTxResponse endpoint performance across all protocols
func checkGetTxResponse(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	// Test all protocols independently using discovered transaction data
	checkGetTxResponseRPC(chainEndpoints, blockData)
	checkGetTxResponseAPI(chainEndpoints, blockData)
	checkGetTxResponseGRPC(chainEndpoints, blockData)
}

// checkGetTxResponsesForEventRPC tests event-based transaction queries via RPC
func checkGetTxResponsesForEventRPC(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	// Run multiple iterations for statistical analysis
	iterations := config.TestConfig.TestIterations
	if iterations <= 0 {
		iterations = 1
	}

	runMultipleIterationsSimple(
		func() { checkGetTxResponsesForEventRPCSingle(chainEndpoints, blockData) },
		chainEndpoints.Name, "GetTxResponsesForEvent-RPC", iterations)
}

// checkGetTxResponsesForEventRPCSingle runs a single RPC GetTxResponsesForEvent test
func checkGetTxResponsesForEventRPCSingle(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	start := time.Now()

	// Use discovered block height for more targeted queries
	var rpcURL string
	if blockData.HasTransactions {
		// All chains perform better with exact height queries
		rpcURL = fmt.Sprintf("%s/tx_search?query=\"tx.height=%d\"&per_page=5", chainEndpoints.RPC, blockData.Height)
	} else {
		// Fallback to broader search
		rpcURL = fmt.Sprintf("%s/tx_search?query=\"message.action='%%2Fcosmos.bank.v1beta1.MsgSend'\"&per_page=5", chainEndpoints.RPC)
	}

	reqCtx, cancel := context.WithTimeout(context.Background(), config.TestConfig.Timeout)
	defer cancel()
	resp, err := httpGetWithRetry(reqCtx, rpcURL, config.TestConfig.RetryAttempts)
	latency := time.Since(start)

	if err != nil {
		logResult(chainEndpoints.Name, "GetTxResponsesForEvent-RPC", "FAIL", fmt.Sprintf("RPC request failed: %v", err), latency, nil)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		logResult(chainEndpoints.Name, "GetTxResponsesForEvent-RPC", "FAIL", fmt.Sprintf("RPC HTTP %d", resp.StatusCode), latency, nil)
		return
	}

	var rpcResp struct {
		Result struct {
			Txs []interface{} `json:"txs"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		logResult(chainEndpoints.Name, "GetTxResponsesForEvent-RPC", "FAIL", fmt.Sprintf("Invalid RPC response: %v", err), latency, nil)
		return
	}

	totalTxs := len(rpcResp.Result.Txs)
	status := "PASS"
	message := "RPC event queries working"

	if totalTxs == 0 {
		status = "FAIL"
		message = "Indexing issue - no transactions found despite testing against blocks with known transactions"
	}

	if latency > config.Thresholds.GetTxResponsesForEvent.Pass {
		if status == "PASS" {
			status = getThresholdStatus(latency, config.Thresholds.GetTxResponsesForEvent)
			switch status {
			case "WARN":
				message += " (slow response)"
			case "FAIL":
				message += " (very slow response)"
			}
		}
	}

	logResult(chainEndpoints.Name, "GetTxResponsesForEvent-RPC", status, message, latency,
		map[string]interface{}{
			"total_transactions": totalTxs,
			"endpoint_type":      "RPC",
			"search_block":       blockData.Height,
			"has_tx_data":        blockData.HasTransactions,
		})
}

// checkGetTxResponsesForEventAPI tests event-based transaction queries via REST API
func checkGetTxResponsesForEventAPI(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	// Run multiple iterations for statistical analysis
	iterations := config.TestConfig.TestIterations
	if iterations <= 0 {
		iterations = 1
	}

	runMultipleIterationsSimple(
		func() { checkGetTxResponsesForEventAPISingle(chainEndpoints, blockData) },
		chainEndpoints.Name, "GetTxResponsesForEvent-API", iterations)
}

// checkGetTxResponsesForEventAPISingle runs a single API GetTxResponsesForEvent test
func checkGetTxResponsesForEventAPISingle(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	start := time.Now()

	// Use discovered block height for more targeted queries
	var apiURL string

	// Get chain configuration for API query style
	chainKey := strings.ToLower(chainEndpoints.Name)
	chainConfig, exists := config.Chains[chainKey]
	if !exists {
		// Fallback to default behavior if chain not in config
		apiURL = fmt.Sprintf("%s/cosmos/tx/v1beta1/txs?query=message.action='%%2Fcosmos.bank.v1beta1.MsgSend'&limit=5", chainEndpoints.API)
	} else {
		if blockData.HasTransactions {
			// Use config-driven query parameter style based on SDK version and implementation
			if chainConfig.APIQueryStyle == "events" {
				// SDK versions like v0.47.x prefer 'events' parameter format
				apiURL = fmt.Sprintf("%s/cosmos/tx/v1beta1/txs?events=tx.height=%d&limit=5", chainEndpoints.API, blockData.Height)
			} else {
				// SDK versions like v0.50.x use 'query' parameter format (default)
				apiURL = fmt.Sprintf("%s/cosmos/tx/v1beta1/txs?query=tx.height=%d&limit=5", chainEndpoints.API, blockData.Height)
			}
		} else {
			// Fallback to broader search with message action filter using configured style
			if chainConfig.APIQueryStyle == "events" {
				apiURL = fmt.Sprintf("%s/cosmos/tx/v1beta1/txs?events=message.action='%%2Fcosmos.bank.v1beta1.MsgSend'&limit=5", chainEndpoints.API)
			} else {
				apiURL = fmt.Sprintf("%s/cosmos/tx/v1beta1/txs?query=message.action='%%2Fcosmos.bank.v1beta1.MsgSend'&limit=5", chainEndpoints.API)
			}
		}
	}

	reqCtx, cancel := context.WithTimeout(context.Background(), config.TestConfig.Timeout)
	defer cancel()
	resp, err := httpGetWithRetry(reqCtx, apiURL, config.TestConfig.RetryAttempts)
	latency := time.Since(start)

	if err != nil {
		logResult(chainEndpoints.Name, "GetTxResponsesForEvent-API", "FAIL", fmt.Sprintf("API request failed: %v", err), latency, nil)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		logResult(chainEndpoints.Name, "GetTxResponsesForEvent-API", "FAIL", fmt.Sprintf("API HTTP %d", resp.StatusCode), latency, nil)
		return
	}

	var apiResp struct {
		TxResponses []interface{} `json:"tx_responses"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		logResult(chainEndpoints.Name, "GetTxResponsesForEvent-API", "FAIL", fmt.Sprintf("Invalid API response: %v", err), latency, nil)
		return
	}

	totalTxs := len(apiResp.TxResponses)
	status := "PASS"
	message := "REST API event queries working"

	if totalTxs == 0 {
		status = "FAIL"
		message = "Indexing issue - no transactions found despite testing against blocks with known transactions"
	}

	if latency > config.Thresholds.GetTxResponsesForEvent.Pass {
		if status == "PASS" {
			status = getThresholdStatus(latency, config.Thresholds.GetTxResponsesForEvent)
			switch status {
			case "WARN":
				message += " (slow response)"
			case "FAIL":
				message += " (very slow response)"
			}
		}
	}

	logResult(chainEndpoints.Name, "GetTxResponsesForEvent-API", status, message, latency,
		map[string]interface{}{
			"total_transactions": totalTxs,
			"endpoint_type":      "REST API",
			"search_block":       blockData.Height,
			"has_tx_data":        blockData.HasTransactions,
		})
}

// checkGetTxResponsesForEventGRPC tests event-based transaction queries via GRPC using actual Cosmos SDK service calls
func checkGetTxResponsesForEventGRPC(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {

	// Run multiple iterations for statistical analysis
	iterations := config.TestConfig.TestIterations
	if iterations <= 0 {
		iterations = 1
	}

	runMultipleIterationsSimple(
		func() { checkGetTxResponsesForEventGRPCSingle(chainEndpoints, blockData) },
		chainEndpoints.Name, "GetTxResponsesForEvent-GRPC", iterations)
}

// checkGetTxResponsesForEventGRPCSingle runs a single GRPC GetTxResponsesForEvent test
func checkGetTxResponsesForEventGRPCSingle(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	start := time.Now()

	conn, err := getGRPCConnection(chainEndpoints.GRPC)
	if err != nil {
		logResult(chainEndpoints.Name, "GetTxResponsesForEvent-GRPC", "WARN", fmt.Sprintf("GRPC endpoint not accessible: %v", err), time.Since(start),
			map[string]interface{}{
				"endpoint_type": "GRPC",
				"note":          "GRPC endpoint may not be exposed or configured",
			})
		return
	}
	defer conn.Close()

	// Create Tx service client and perform actual GetTxsEvent call
	txClient := txtypes.NewServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Create version-aware request based on SDK version
	var req *txtypes.GetTxsEventRequest

	// Get chain configuration for SDK version
	chainKey := strings.ToLower(chainEndpoints.Name)
	chainConfig, exists := config.Chains[chainKey]
	if !exists {
		logResult(chainEndpoints.Name, "GetTxResponsesForEvent-GRPC", "FAIL", "Chain configuration not found", time.Since(start), nil)
		return
	}

	// Use block height-based event query for better reliability
	heightQuery := fmt.Sprintf("tx.height=%d", blockData.Height)

	if strings.HasPrefix(chainConfig.SDKVersion, "v0.47") {
		// For Cosmos SDK v0.47.x (like Pryzm) - use Events field
		req = &txtypes.GetTxsEventRequest{
			Events: []string{heightQuery},
			Page:   1,
			Limit:  5,
		}
	} else {
		// For Cosmos SDK v0.50.x+ (like Neutron, dYdX) - use Query field
		req = &txtypes.GetTxsEventRequest{
			Query: heightQuery,
			Page:  1,
			Limit: 5,
		}
	}

	resp, err := txClient.GetTxsEvent(ctx, req)
	latency := time.Since(start)

	if err != nil {
		// Handle common GRPC errors gracefully
		errStr := err.Error()
		if strings.Contains(errStr, "Unimplemented") {
			logResult(chainEndpoints.Name, "GetTxResponsesForEvent-GRPC", "WARN", "GetTxsEvent service not implemented on this endpoint", latency,
				map[string]interface{}{
					"endpoint_type": "GRPC",
					"error_type":    "service_unavailable",
					"search_block":  blockData.Height,
					"note":          "Chain may not expose Tx event service via GRPC",
				})
		} else if strings.Contains(errStr, "NotFound") || strings.Contains(errStr, "no transactions found") {
			logResult(chainEndpoints.Name, "GetTxResponsesForEvent-GRPC", "WARN", "No transactions found for event query via GRPC", latency,
				map[string]interface{}{
					"endpoint_type": "GRPC",
					"search_block":  blockData.Height,
					"error_type":    "no_results",
					"note":          "May indicate indexing issues or empty block",
				})
		} else {
			logResult(chainEndpoints.Name, "GetTxResponsesForEvent-GRPC", "FAIL", fmt.Sprintf("GetTxsEvent GRPC call failed: %v", err), latency,
				map[string]interface{}{
					"endpoint_type": "GRPC",
					"search_block":  blockData.Height,
					"error_type":    "call_failed",
				})
		}
		return
	}

	if resp == nil {
		logResult(chainEndpoints.Name, "GetTxResponsesForEvent-GRPC", "FAIL", "GetTxsEvent returned nil response", latency,
			map[string]interface{}{
				"endpoint_type": "GRPC",
				"search_block":  blockData.Height,
			})
		return
	}

	// Handle both TxResponses (newer) and Txs (older) fields
	var totalTxs int
	if resp.TxResponses != nil {
		totalTxs = len(resp.TxResponses)
	} else if resp.Txs != nil {
		totalTxs = len(resp.Txs)
	} else {
		// Both fields are nil, but response exists - this could still be valid (empty result)
		totalTxs = 0
	}

	// Successful GRPC event-based transaction query
	status := "PASS"
	message := "DEBUG-EVENT-SUCCESS: Event queries working via GRPC service"

	if totalTxs == 0 {
		status = "WARN"
		message = "GRPC service accessible but no transactions found - possible indexing issue"
	}

	if latency > config.Thresholds.GetTxResponsesForEvent.Pass {
		if status == "PASS" {
			status = getThresholdStatus(latency, config.Thresholds.GetTxResponsesForEvent)
			switch status {
			case "WARN":
				message += " (slow response)"
			case "FAIL":
				message += " (very slow response)"
			}
		}
	}

	logResult(chainEndpoints.Name, "GetTxResponsesForEvent-GRPC", status, message, latency,
		map[string]interface{}{
			"endpoint_type":      "GRPC",
			"total_transactions": totalTxs,
			"search_block":       blockData.Height,
			"has_tx_data":        blockData.HasTransactions,
		})
}

// checkGetTxResponsesForEvent tests event-based transaction queries across all protocols
func checkGetTxResponsesForEvent(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	// Test all protocols independently using discovered transaction data
	checkGetTxResponsesForEventRPC(chainEndpoints, blockData)
	checkGetTxResponsesForEventAPI(chainEndpoints, blockData)
	checkGetTxResponsesForEventGRPC(chainEndpoints, blockData)
}

// checkBankQueryBalanceAPI tests balance query performance via REST API
func checkBankQueryBalanceAPI(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	// Run multiple iterations for statistical analysis
	iterations := config.TestConfig.TestIterations
	if iterations <= 0 {
		iterations = 1
	}

	runMultipleIterationsSimple(
		func() { checkBankQueryBalanceAPISingle(chainEndpoints, blockData) },
		chainEndpoints.Name, "BankQueryBalance-API", iterations)
}

// checkBankQueryBalanceAPISingle runs a single API BankQueryBalance test
func checkBankQueryBalanceAPISingle(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	start := time.Now()

	// Use discovered address or fallback
	testAddress := blockData.TestAddress
	if testAddress == "" {
		testAddress = getTestAddress(chainEndpoints)
	}
	if testAddress == "" {
		logResult(chainEndpoints.Name, "BankQueryBalance-API", "FAIL", "Could not find test address", time.Since(start), nil)
		return
	}

	url := fmt.Sprintf("%s/cosmos/bank/v1beta1/balances/%s", chainEndpoints.API, testAddress)
	reqCtx, cancel := context.WithTimeout(context.Background(), config.TestConfig.Timeout)
	defer cancel()
	resp, err := httpGetWithRetry(reqCtx, url, config.TestConfig.RetryAttempts)
	latency := time.Since(start)

	if err != nil {
		logResult(chainEndpoints.Name, "BankQueryBalance-API", "FAIL", fmt.Sprintf("HTTP error: %v", err), latency, nil)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		logResult(chainEndpoints.Name, "BankQueryBalance-API", "FAIL", fmt.Sprintf("HTTP %d", resp.StatusCode), latency, nil)
		return
	}

	var balanceResp struct {
		Balances []struct {
			Denom  string `json:"denom"`
			Amount string `json:"amount"`
		} `json:"balances"`
		Pagination interface{} `json:"pagination"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&balanceResp); err != nil {
		logResult(chainEndpoints.Name, "BankQueryBalance-API", "FAIL", fmt.Sprintf("Invalid JSON: %v", err), latency, nil)
		return
	}

	status := getThresholdStatus(latency, config.Thresholds.BankQueryBalance)
	message := fmt.Sprintf("Balance query successful via REST API (%d balances)", len(balanceResp.Balances))

	switch status {
	case "WARN":
		message += " (slow response)"
	case "FAIL":
		message += " (very slow response)"
	}

	logResult(chainEndpoints.Name, "BankQueryBalance-API", status, message, latency,
		map[string]interface{}{
			"test_address":  testAddress,
			"balance_count": len(balanceResp.Balances),
			"endpoint_type": "REST API",
		})
}

// checkBankQueryBalanceGRPC tests balance query performance via GRPC using actual Cosmos SDK service calls
func checkBankQueryBalanceGRPC(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {

	// Run multiple iterations for statistical analysis
	iterations := config.TestConfig.TestIterations
	if iterations <= 0 {
		iterations = 1
	}

	runMultipleIterationsSimple(
		func() { checkBankQueryBalanceGRPCSingle(chainEndpoints, blockData) },
		chainEndpoints.Name, "BankQueryBalance-GRPC", iterations)
}

// checkBankQueryBalanceGRPCSingle runs a single GRPC BankQueryBalance test
func checkBankQueryBalanceGRPCSingle(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	start := time.Now()

	testAddress := blockData.TestAddress
	if testAddress == "" {
		testAddress = getTestAddress(chainEndpoints)
	}
	if testAddress == "" {
		logResult(chainEndpoints.Name, "BankQueryBalance-GRPC", "FAIL", "Could not find test address", time.Since(start), nil)
		return
	}

	conn, err := getGRPCConnection(chainEndpoints.GRPC)
	if err != nil {
		logResult(chainEndpoints.Name, "BankQueryBalance-GRPC", "WARN", fmt.Sprintf("GRPC endpoint not accessible: %v", err), time.Since(start),
			map[string]interface{}{
				"test_address":  testAddress,
				"endpoint_type": "GRPC",
				"note":          "GRPC endpoint may not be exposed or configured",
			})
		return
	}
	defer conn.Close()

	// Create Bank query client and perform actual AllBalances call
	bankClient := banktypes.NewQueryClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req := &banktypes.QueryAllBalancesRequest{
		Address: testAddress,
		Pagination: &query.PageRequest{
			Limit: 100, // Get first 100 balances
		},
	}

	resp, err := bankClient.AllBalances(ctx, req)
	latency := time.Since(start)

	if err != nil {
		// Handle common GRPC errors gracefully
		errStr := err.Error()
		if strings.Contains(errStr, "Unimplemented") {
			logResult(chainEndpoints.Name, "BankQueryBalance-GRPC", "WARN", "Bank AllBalances service not implemented on this endpoint", latency,
				map[string]interface{}{
					"test_address":  testAddress,
					"endpoint_type": "GRPC",
					"error_type":    "service_unavailable",
					"note":          "Chain may not expose Bank query service via GRPC",
				})
		} else if strings.Contains(errStr, "NotFound") || strings.Contains(errStr, "account not found") {
			logResult(chainEndpoints.Name, "BankQueryBalance-GRPC", "WARN", "Account not found via GRPC bank service", latency,
				map[string]interface{}{
					"test_address":  testAddress,
					"endpoint_type": "GRPC",
					"error_type":    "account_not_found",
					"note":          "Test address may not exist or not be indexed",
				})
		} else {
			logResult(chainEndpoints.Name, "BankQueryBalance-GRPC", "FAIL", fmt.Sprintf("AllBalances GRPC call failed: %v", err), latency,
				map[string]interface{}{
					"test_address":  testAddress,
					"endpoint_type": "GRPC",
					"error_type":    "call_failed",
				})
		}
		return
	}

	if resp == nil {
		logResult(chainEndpoints.Name, "BankQueryBalance-GRPC", "FAIL", "AllBalances returned nil response", latency,
			map[string]interface{}{
				"test_address":  testAddress,
				"endpoint_type": "GRPC",
			})
		return
	}

	// Successful GRPC balance query - handle nil balances (which is valid for accounts with no balances)
	balanceCount := 0
	if resp.Balances != nil {
		balanceCount = len(resp.Balances)
	}

	status := getThresholdStatus(latency, config.Thresholds.BankQueryBalance)
	message := fmt.Sprintf("DEBUG-BANK-SUCCESS: Balance query successful via GRPC service (%d balances)", balanceCount)

	switch status {
	case "WARN":
		message += " (slow response)"
	case "FAIL":
		message += " (very slow response)"
	}

	logResult(chainEndpoints.Name, "BankQueryBalance-GRPC", status, message, latency,
		map[string]interface{}{
			"test_address":  testAddress,
			"endpoint_type": "GRPC",
			"balance_count": balanceCount,
		})
}

// checkAuthServiceGRPC tests account query performance via GRPC using actual Cosmos SDK service calls
func checkAuthServiceGRPC(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	// Run multiple iterations for statistical analysis
	iterations := config.TestConfig.TestIterations
	if iterations <= 0 {
		iterations = 1
	}

	runMultipleIterationsSimple(
		func() { checkAuthServiceGRPCSingle(chainEndpoints, blockData) },
		chainEndpoints.Name, "AuthService-GRPC", iterations)
}

// checkAuthServiceGRPCSingle runs a single GRPC AuthService test
func checkAuthServiceGRPCSingle(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	start := time.Now()

	testAddress := blockData.TestAddress
	if testAddress == "" {
		testAddress = getTestAddress(chainEndpoints)
	}
	if testAddress == "" {
		logResult(chainEndpoints.Name, "AuthService-GRPC", "FAIL", "Could not find test address", time.Since(start), nil)
		return
	}

	conn, err := getGRPCConnection(chainEndpoints.GRPC)
	if err != nil {
		logResult(chainEndpoints.Name, "AuthService-GRPC", "WARN", fmt.Sprintf("GRPC endpoint not accessible: %v", err), time.Since(start),
			map[string]interface{}{
				"test_address":  testAddress,
				"endpoint_type": "GRPC",
				"note":          "GRPC endpoint may not be exposed or configured",
			})
		return
	}
	defer conn.Close()

	// Create Auth query client and perform actual Account call
	authClient := authtypes.NewQueryClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req := &authtypes.QueryAccountRequest{
		Address: testAddress,
	}

	resp, err := authClient.Account(ctx, req)
	latency := time.Since(start)

	if err != nil {
		// Handle common GRPC errors gracefully
		errStr := err.Error()
		if strings.Contains(errStr, "Unimplemented") {
			logResult(chainEndpoints.Name, "AuthService-GRPC", "WARN", "Auth Account service not implemented on this endpoint", latency,
				map[string]interface{}{
					"test_address":  testAddress,
					"endpoint_type": "GRPC",
					"error_type":    "service_unavailable",
					"note":          "Chain may not expose Auth query service via GRPC",
				})
		} else if strings.Contains(errStr, "NotFound") || strings.Contains(errStr, "account not found") {
			logResult(chainEndpoints.Name, "AuthService-GRPC", "WARN", "Account not found via GRPC auth service", latency,
				map[string]interface{}{
					"test_address":  testAddress,
					"endpoint_type": "GRPC",
					"error_type":    "account_not_found",
					"note":          "Test address may not exist or not have been used in transactions",
				})
		} else {
			logResult(chainEndpoints.Name, "AuthService-GRPC", "FAIL", fmt.Sprintf("Account GRPC call failed: %v", err), latency,
				map[string]interface{}{
					"test_address":  testAddress,
					"endpoint_type": "GRPC",
					"error_type":    "call_failed",
				})
		}
		return
	}

	if resp == nil {
		logResult(chainEndpoints.Name, "AuthService-GRPC", "FAIL", "Account query returned nil response", latency,
			map[string]interface{}{
				"test_address":  testAddress,
				"endpoint_type": "GRPC",
			})
		return
	}

	if resp.Account == nil {
		logResult(chainEndpoints.Name, "AuthService-GRPC", "WARN", "Account not found via GRPC auth service", latency,
			map[string]interface{}{
				"test_address":  testAddress,
				"endpoint_type": "GRPC",
				"error_type":    "account_not_found",
				"note":          "Test address may not exist or not have been used in transactions",
			})
		return
	}

	// Successful GRPC account query
	status := getThresholdStatus(latency, config.Thresholds.BankQueryBalance) // Reuse balance threshold
	message := "Account query successful via GRPC service"

	switch status {
	case "WARN":
		message += " (slow response)"
	case "FAIL":
		message += " (very slow response)"
	}

	logResult(chainEndpoints.Name, "AuthService-GRPC", status, message, latency,
		map[string]interface{}{
			"test_address":  testAddress,
			"endpoint_type": "GRPC",
			"account_type":  resp.Account.TypeUrl,
		})
}

// checkBankQueryBalance tests balance query performance across all protocols
func checkBankQueryBalance(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	// Test both API and GRPC protocols
	checkBankQueryBalanceAPI(chainEndpoints, blockData)
	checkBankQueryBalanceGRPC(chainEndpoints, blockData)
}

// checkEventIndexing verifies that event indexing is properly configured and working
func checkEventIndexing(chainEndpoints ChainEndpoints) {
	// Run multiple iterations for statistical analysis
	iterations := config.TestConfig.TestIterations
	if iterations <= 0 {
		iterations = 1
	}

	// Event Indexing is a validation test, run once not multiple iterations
	checkEventIndexingSingle(chainEndpoints)
}

func checkEventIndexingSingle(chainEndpoints ChainEndpoints) {
	start := time.Now()

	// Check if we already verified event indexing works via previous tests
	// Look through previous results to see if GetTxResponsesForEvent tests passed
	rpcEventIndexingWorking := false
	apiEventIndexingWorking := false
	grpcEventIndexingWorking := false

	for _, result := range results {
		if result.Chain == chainEndpoints.Name {
			if result.Test == "GetTxResponsesForEvent-RPC" && result.Status == "PASS" {
				rpcEventIndexingWorking = true
			}
			if result.Test == "GetTxResponsesForEvent-API" && result.Status == "PASS" {
				apiEventIndexingWorking = true
			}
			if result.Test == "GetTxResponsesForEvent-GRPC" && result.Status == "PASS" {
				grpcEventIndexingWorking = true
			}
		}
	}

	var status string
	var message string
	var details map[string]interface{}

	workingProtocols := 0
	if rpcEventIndexingWorking {
		workingProtocols++
	}
	if apiEventIndexingWorking {
		workingProtocols++
	}
	if grpcEventIndexingWorking {
		workingProtocols++
	}

	if workingProtocols >= 2 {
		status = "PASS"
		message = fmt.Sprintf("Event indexing verified via %d protocols (RPC: %v, API: %v, GRPC: %v)",
			workingProtocols, rpcEventIndexingWorking, apiEventIndexingWorking, grpcEventIndexingWorking)
		details = map[string]interface{}{
			"method":            "previous_test_verification",
			"rpc_working":       rpcEventIndexingWorking,
			"api_working":       apiEventIndexingWorking,
			"grpc_working":      grpcEventIndexingWorking,
			"working_protocols": workingProtocols,
		}
	} else if workingProtocols == 1 {
		status = "WARN"
		message = fmt.Sprintf("Event indexing partially working (%d/3 protocols working)", workingProtocols)
		details = map[string]interface{}{
			"method":            "previous_test_verification",
			"rpc_working":       rpcEventIndexingWorking,
			"api_working":       apiEventIndexingWorking,
			"grpc_working":      grpcEventIndexingWorking,
			"working_protocols": workingProtocols,
		}
	} else {
		status = "FAIL"
		message = "Event indexing not working on any protocol"
		details = map[string]interface{}{
			"method":            "previous_test_verification",
			"rpc_working":       false,
			"api_working":       false,
			"grpc_working":      false,
			"working_protocols": 0,
		}
	}

	latency := time.Since(start)
	logResult(chainEndpoints.Name, "Event Indexing", status, message, latency, details)
}

// checkWebSocketConnection tests WebSocket connectivity and subscription functionality
func checkWebSocketConnection(chainEndpoints ChainEndpoints) {
	// Run multiple iterations for statistical analysis
	iterations := config.TestConfig.TestIterations
	if iterations <= 0 {
		iterations = 1
	}

	runMultipleIterationsSimple(
		func() { checkWebSocketConnectionSingle(chainEndpoints) },
		chainEndpoints.Name, "WebSocket Connection", iterations)
}

// checkWebSocketConnectionSingle runs a single WebSocket connection test
func checkWebSocketConnectionSingle(chainEndpoints ChainEndpoints) {
	start := time.Now()

	if chainEndpoints.WebSocket == "" {
		logResult(chainEndpoints.Name, "WebSocket Connection", "FAIL", "WebSocket endpoint not configured", time.Since(start), nil)
		return
	}

	// Parse WebSocket URL
	wsURL, err := url.Parse(chainEndpoints.WebSocket)
	if err != nil {
		logResult(chainEndpoints.Name, "WebSocket Connection", "FAIL", fmt.Sprintf("Invalid WebSocket URL: %v", err), time.Since(start), nil)
		return
	}

	// Create WebSocket dialer with timeout
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	dialer.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: false,
		MinVersion:         tls.VersionTLS12,
	}

	// Connect to WebSocket
	conn, resp, err := dialer.Dial(wsURL.String(), nil)
	connectLatency := time.Since(start)

	if err != nil {
		var statusCode int
		if resp != nil {
			statusCode = resp.StatusCode
		}
		logResult(chainEndpoints.Name, "WebSocket Connection", "FAIL",
			fmt.Sprintf("Connection failed (HTTP %d): %v", statusCode, err), connectLatency, nil)
		return
	}
	defer conn.Close()

	// Test basic subscription to new blocks
	subscribeStart := time.Now()
	subscription := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "subscribe",
		"id":      1,
		"params": map[string]interface{}{
			"query": "tm.event='NewBlock'",
		},
	}

	// Send subscription request
	if err := conn.WriteJSON(subscription); err != nil {
		logResult(chainEndpoints.Name, "WebSocket Connection", "WARN",
			fmt.Sprintf("Connected but subscription failed: %v", err), connectLatency,
			map[string]interface{}{
				"connection_successful": true,
				"subscription_error":    err.Error(),
			})
		return
	}

	// Set read timeout for response
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		logResult(chainEndpoints.Name, "WebSocket Connection", "FAIL", fmt.Sprintf("Failed to set read deadline: %v", err), 0, nil)
		return
	}

	// Read subscription confirmation
	var response map[string]interface{}
	if err := conn.ReadJSON(&response); err != nil {
		logResult(chainEndpoints.Name, "WebSocket Connection", "WARN",
			fmt.Sprintf("Connected but no subscription response: %v", err), connectLatency,
			map[string]interface{}{
				"connection_successful": true,
				"timeout_error":         err.Error(),
			})
		return
	}

	subscribeLatency := time.Since(subscribeStart)
	totalLatency := time.Since(start)

	// Check if subscription was successful
	if result, ok := response["result"]; ok && result != nil {
		status := getThresholdStatus(totalLatency, config.Thresholds.GetTxResponse) // Reuse existing threshold

		logResult(chainEndpoints.Name, "WebSocket Connection", status,
			"WebSocket connection and subscription successful", totalLatency,
			map[string]interface{}{
				"connect_latency":   connectLatency.Milliseconds(),
				"subscribe_latency": subscribeLatency.Milliseconds(),
				"subscription_id":   result,
				"endpoint_type":     "WebSocket",
			})
	} else {
		logResult(chainEndpoints.Name, "WebSocket Connection", "WARN",
			"Connected but subscription failed", totalLatency,
			map[string]interface{}{
				"connect_latency":   connectLatency.Milliseconds(),
				"subscribe_latency": subscribeLatency.Milliseconds(),
				"response":          response,
			})
	}
}

// Helper function to get a test address for balance queries using config-driven approach
func getTestAddress(chainEndpoints ChainEndpoints) string {
	// Get chain configuration
	chainKey := strings.ToLower(chainEndpoints.Name)
	chainConfig, exists := config.Chains[chainKey]
	if !exists {
		return ""
	}

	// 1. First try config-provided test addresses
	if len(chainConfig.TestAddresses) > 0 {
		return chainConfig.TestAddresses[0] // Use first available test address
	}

	// 2. If no test addresses in config, try dynamic discovery strategies
	// Try transaction-based discovery if fallback query height is configured
	if chainConfig.QueryStrategies.FallbackQueryHeight > 0 {
		address := getAddressFromTransactionQuery(chainEndpoints, chainConfig.QueryStrategies.FallbackQueryHeight)
		if address != "" {
			return address
		}
	}

	// 3. Fall back to generic validator address discovery
	return getValidatorAddress(chainEndpoints)
}

// Helper function to get an address from transaction queries at a specific height
func getAddressFromTransactionQuery(chainEndpoints ChainEndpoints, queryHeight int64) string {
	// Use the configured fallback query height for transaction search
	reqCtx, cancel := context.WithTimeout(context.Background(), config.TestConfig.Timeout)
	defer cancel()
	resp, err := httpGetWithRetry(reqCtx, fmt.Sprintf("%s/tx_search?query=\"tx.height=%d\"&per_page=1", chainEndpoints.RPC, queryHeight), config.TestConfig.RetryAttempts)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var rpcResp struct {
		Result struct {
			Txs []struct {
				TxResult struct {
					Events []struct {
						Type       string `json:"type"`
						Attributes []struct {
							Key   string `json:"key"`
							Value string `json:"value"`
						} `json:"attributes"`
					} `json:"events"`
				} `json:"tx_result"`
			} `json:"txs"`
		} `json:"result"`
	}

	if json.NewDecoder(resp.Body).Decode(&rpcResp) == nil && len(rpcResp.Result.Txs) > 0 {
		// Look for sender/recipient addresses in transaction events
		for _, tx := range rpcResp.Result.Txs {
			for _, event := range tx.TxResult.Events {
				for _, attr := range event.Attributes {
					if (attr.Key == "sender" || attr.Key == "recipient") && len(attr.Value) > 10 {
						// Basic validation that this looks like a valid address
						return attr.Value
					}
				}
			}
		}
	}

	return ""
}

// Helper function to get a validator address from staking module
func getValidatorAddress(chainEndpoints ChainEndpoints) string {
	reqCtx, cancel := context.WithTimeout(context.Background(), config.TestConfig.Timeout)
	defer cancel()
	resp, err := httpGetWithRetry(reqCtx, chainEndpoints.API+"/cosmos/staking/v1beta1/validators?status=BOND_STATUS_BONDED&limit=1", config.TestConfig.RetryAttempts)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var validatorResp struct {
		Validators []struct {
			OperatorAddress string `json:"operator_address"`
		} `json:"validators"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&validatorResp); err != nil {
		return ""
	}

	if len(validatorResp.Validators) > 0 {
		// Use the operator address directly (could be converted to account address if needed)
		return validatorResp.Validators[0].OperatorAddress
	}

	return ""
}

// runBenchmarks executes all benchmark tests for a given chain
func runBenchmarks(chain string) {
	chainEndpoints, exists := getChainEndpoints(chain)
	if !exists {
		fmt.Printf("❌ Unknown chain: %s\n", chain)
		fmt.Printf("Available chains in config: %s\n", strings.Join(getAvailableChains(), ", "))
		return
	}

	fmt.Printf("\n🔬 Running Node Benchmarks for %s\n", chainEndpoints.Name)
	fmt.Printf("Endpoints: RPC=%s, API=%s, GRPC=%s, WS=%s\n\n", chainEndpoints.RPC, chainEndpoints.API, chainEndpoints.GRPC, chainEndpoints.WebSocket)

	// First, discover blocks with transactions for targeted testing
	blockData := findBlockWithTransactions(chainEndpoints)
	if blockData == nil {
		fmt.Printf("❌ Failed to discover transaction data for %s\n", chainEndpoints.Name)
		return
	}

	// Run all benchmark tests using discovered transaction data
	// Respect the parallel_execution configuration setting
	if config.TestConfig.ParallelExecution {
		runBenchmarksConcurrently(chainEndpoints, blockData)
	} else {
		runBenchmarksSequentially(chainEndpoints, blockData)
	}
}

// runBenchmarksSequentially executes all benchmark tests one by one to avoid race conditions
func runBenchmarksSequentially(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	// Run tests sequentially to avoid race conditions with the global results slice
	checkDataRetention(chainEndpoints)
	checkGetTxResponse(chainEndpoints, blockData)
	checkGetTxResponsesForEvent(chainEndpoints, blockData)
	checkBankQueryBalance(chainEndpoints, blockData)
	checkAuthServiceGRPC(chainEndpoints, blockData)
	checkWebSocketConnection(chainEndpoints)

	// Now run event indexing test which depends on results from the previous tests
	checkEventIndexing(chainEndpoints)
}

// runBenchmarksConcurrently executes all benchmark tests in parallel for optimal performance
func runBenchmarksConcurrently(chainEndpoints ChainEndpoints, blockData *BlockWithTransactions) {
	var wg sync.WaitGroup

	// Define tests that can run fully concurrently (don't depend on other test results)
	independentTests := []func(){
		func() {
			defer wg.Done()
			checkDataRetention(chainEndpoints)
		},
		func() {
			defer wg.Done()
			checkGetTxResponse(chainEndpoints, blockData)
		},
		func() {
			defer wg.Done()
			checkGetTxResponsesForEvent(chainEndpoints, blockData)
		},
		func() {
			defer wg.Done()
			checkBankQueryBalance(chainEndpoints, blockData)
		},
		func() {
			defer wg.Done()
			checkAuthServiceGRPC(chainEndpoints, blockData)
		},
		func() {
			defer wg.Done()
			checkWebSocketConnection(chainEndpoints)
		},
	}

	// Start all independent tests concurrently
	for _, test := range independentTests {
		wg.Add(1)
		go test()
	}

	// Wait for all independent tests to complete
	wg.Wait()

	// Now run event indexing test which depends on results from the previous tests
	checkEventIndexing(chainEndpoints)
}

// generateReport creates a summary report of all benchmark results
func generateReport() {
	fmt.Printf("\n" + strings.Repeat("=", 80) + "\n")
	fmt.Printf("📊 NODE BENCHMARK REPORT\n")
	fmt.Printf("Generated: %s\n", time.Now().Format(time.RFC3339))
	fmt.Printf(strings.Repeat("=", 80) + "\n\n")

	// Group results by chain (thread-safe read)
	resultsMu.Lock()
	chainResults := make(map[string][]BenchmarkResult)
	for _, result := range results {
		chainResults[result.Chain] = append(chainResults[result.Chain], result)
	}
	resultsMu.Unlock()

	for chain, chainTests := range chainResults {
		fmt.Printf("🔗 %s CHAIN RESULTS:\n\n", strings.ToUpper(chain))

		// Generate protocol-organized sections with integrated statistics
		generateProtocolSectionsWithStats(chainTests)

		// Generate summary
		passCount := 0
		warnCount := 0
		failCount := 0

		for _, test := range chainTests {
			switch test.Status {
			case "PASS":
				passCount++
			case "WARN":
				warnCount++
			case "FAIL":
				failCount++
			}
		}

		fmt.Printf("📈 SUMMARY: %d passed, %d warnings, %d failed\n\n", passCount, warnCount, failCount)
	}
}

// generateProtocolSectionsWithStats creates protocol-organized sections with integrated statistics
func generateProtocolSectionsWithStats(chainTests []BenchmarkResult) {
	// Group results by protocol
	rpcResults := make([]BenchmarkResult, 0)
	apiResults := make([]BenchmarkResult, 0)
	grpcResults := make([]BenchmarkResult, 0)
	wsResults := make([]BenchmarkResult, 0)
	generalResults := make([]BenchmarkResult, 0)

	for _, result := range chainTests {
		if strings.Contains(result.Test, "-RPC") {
			rpcResults = append(rpcResults, result)
		} else if strings.Contains(result.Test, "-API") {
			apiResults = append(apiResults, result)
		} else if strings.Contains(result.Test, "-GRPC") {
			grpcResults = append(grpcResults, result)
		} else if strings.Contains(result.Test, "WebSocket") {
			wsResults = append(wsResults, result)
		} else {
			// General results include Block Discovery, Event Indexing, Data Retention
			generalResults = append(generalResults, result)
		}
	}

	// Print general results first (Block Discovery, Event Indexing)
	if len(generalResults) > 0 {
		fmt.Printf("🔍 GENERAL:\n")
		fmt.Printf("  %-25s %-8s %-s\n", "Test", "Tests", "Details")
		fmt.Printf("  %s\n", strings.Repeat("-", 80))
		for _, result := range generalResults {
			// Hide Event Indexing and Block Discovery - both run silently for setup/validation
			if result.Test == "Data Retention" {
				testCount := 1

				// Check if result has iteration metadata
				if detailsMap, ok := result.Details.(map[string]interface{}); ok {
					if iterations, ok := detailsMap["show_iterations"].(int); ok {
						testCount = iterations
					}
				}

				// Extract retention data from result details
				if detailsMap, ok := result.Details.(map[string]interface{}); ok {
					if retentionDays, hasRetention := detailsMap["retention_days"].(float64); hasRetention {
						if currentHeight, hasCurrent := detailsMap["current_height"].(int64); hasCurrent {
							if earliestHeight, hasEarliest := detailsMap["earliest_height"].(int64); hasEarliest {
								blocks := currentHeight - earliestHeight
								fmt.Printf("  %s %-22s %-8d %d blocks, ~%.1f days\n",
									getStatusIcon(result.Status),
									result.Test,
									testCount,
									blocks, retentionDays)
							} else if testHeight, hasTest := detailsMap["test_height"].(int64); hasTest {
								blocks := currentHeight - testHeight
								fmt.Printf("  %s %-22s %-8d %d blocks, ~%.1f days\n",
									getStatusIcon(result.Status),
									result.Test,
									testCount,
									blocks, retentionDays)
							} else {
								fmt.Printf("  %s %-22s %-8d ~%.1f days\n",
									getStatusIcon(result.Status),
									result.Test,
									testCount,
									retentionDays)
							}
						} else {
							fmt.Printf("  %s %-22s %-8d ~%.1f days\n",
								getStatusIcon(result.Status),
								result.Test,
								testCount,
								retentionDays)
						}
					} else {
						fmt.Printf("  %s %-22s %-8d n/a\n",
							getStatusIcon(result.Status),
							result.Test,
							testCount)
					}
				} else {
					fmt.Printf("  %s %-22s %-8d n/a\n",
						getStatusIcon(result.Status),
						result.Test,
						testCount)
				}
			}
		}
		fmt.Printf("\n")
	}

	// Print RPC results with integrated statistics
	if len(rpcResults) > 0 {
		fmt.Printf("🔌 RPC RESULTS:\n")
		fmt.Printf("  %-25s %-8s %-s\n", "Test", "Tests", "Latency Statistics")
		fmt.Printf("  %s\n", strings.Repeat("-", 80))

		// Group RPC results by test type
		rpcTestGroups := make(map[string][]BenchmarkResult)
		for _, result := range rpcResults {
			testName := strings.Replace(result.Test, "-RPC", "", 1)
			rpcTestGroups[testName] = append(rpcTestGroups[testName], result)
		}

		// Print each test group with full statistics
		for testName, testResults := range rpcTestGroups {
			// Collect latencies from successful tests
			latencies := make([]time.Duration, 0)
			statusIcon := "✅"
			testCount := len(testResults)

			// Check if any result has iteration metadata
			for _, r := range testResults {
				if r.Details != nil {
					if detailsMap, ok := r.Details.(map[string]interface{}); ok {
						if iterations, ok := detailsMap["show_iterations"].(int); ok {
							testCount = iterations
							break
						}
					}
				}
			}

			for _, r := range testResults {
				if r.Latency > 0 && r.Status != "FAIL" {
					latencies = append(latencies, r.Latency)
				}
				if r.Status == "FAIL" {
					statusIcon = "❌"
				} else if r.Status == "WARN" && statusIcon != "❌" {
					statusIcon = "⚠️"
				}
			}

			if len(latencies) > 0 {
				if len(latencies) == 1 {
					// Check if this is aggregated statistics from multiple iterations
					if testCount > 1 {
						// This is aggregated data from multiple iterations, use the metadata stats
						if len(testResults) > 0 && testResults[0].Details != nil {
							if detailsMap, ok := testResults[0].Details.(map[string]interface{}); ok {
								if statsData, ok := detailsMap["stats"].(LatencyStats); ok {
									fmt.Printf("  %s %-22s %-8d min:%dms max:%dms avg:%dms p50:%dms p95:%dms p99:%dms\n",
										statusIcon, testName, testCount,
										statsData.Min.Milliseconds(), statsData.Max.Milliseconds(), statsData.Mean.Milliseconds(),
										statsData.P50.Milliseconds(), statsData.P95.Milliseconds(), statsData.P99.Milliseconds())
								} else {
									// Fallback to single value
									ms := latencies[0].Milliseconds()
									fmt.Printf("  %s %-22s %-8d min:%dms max:%dms avg:%dms p50:%dms p95:%dms p99:%dms\n",
										statusIcon, testName, testCount, ms, ms, ms, ms, ms, ms)
								}
							}
						}
					} else {
						// Single test - show all stats as same value
						ms := latencies[0].Milliseconds()
						fmt.Printf("  %s %-22s %-8d min:%dms max:%dms avg:%dms p50:%dms p95:%dms p99:%dms\n",
							statusIcon, testName, testCount, ms, ms, ms, ms, ms, ms)
					}
				} else {
					// Multiple tests - show real statistics
					stats := calculateLatencyStats(latencies)
					fmt.Printf("  %s %-22s %-8d min:%dms max:%dms avg:%dms p50:%dms p95:%dms p99:%dms\n",
						statusIcon, testName, testCount,
						stats.Min.Milliseconds(), stats.Max.Milliseconds(), stats.Mean.Milliseconds(),
						stats.P50.Milliseconds(), stats.P95.Milliseconds(), stats.P99.Milliseconds())
				}
			} else {
				fmt.Printf("  %s %-22s %-8d n/a (all tests failed)\n",
					statusIcon, testName, testCount)
			}
		}
		fmt.Printf("\n")
	}

	// Print REST API results with integrated statistics
	if len(apiResults) > 0 {
		fmt.Printf("🌐 REST API RESULTS:\n")
		fmt.Printf("  %-25s %-8s %-s\n", "Test", "Tests", "Latency Statistics")
		fmt.Printf("  %s\n", strings.Repeat("-", 80))

		// Group API results by test type
		apiTestGroups := make(map[string][]BenchmarkResult)
		for _, result := range apiResults {
			testName := strings.Replace(result.Test, "-API", "", 1)
			apiTestGroups[testName] = append(apiTestGroups[testName], result)
		}

		// Print each test group with full statistics
		for testName, testResults := range apiTestGroups {
			// Collect latencies from successful tests
			latencies := make([]time.Duration, 0)
			statusIcon := "✅"
			testCount := len(testResults)

			for _, r := range testResults {
				if r.Latency > 0 && r.Status != "FAIL" {
					latencies = append(latencies, r.Latency)
				}
				if r.Status == "FAIL" {
					statusIcon = "❌"
				} else if r.Status == "WARN" && statusIcon != "❌" {
					statusIcon = "⚠️"
				}

				// Check if any result has iteration metadata
				if detailsMap, ok := r.Details.(map[string]interface{}); ok {
					if iterations, ok := detailsMap["show_iterations"].(int); ok {
						testCount = iterations
					}
				}
			}

			if len(latencies) > 0 {
				if len(latencies) == 1 {
					// Single successful test - show all stats as same value
					ms := latencies[0].Milliseconds()
					fmt.Printf("  %s %-22s %-8d min:%dms max:%dms avg:%dms p50:%dms p95:%dms p99:%dms\n",
						statusIcon, testName, testCount, ms, ms, ms, ms, ms, ms)
				} else {
					// Multiple tests - show real statistics
					stats := calculateLatencyStats(latencies)
					fmt.Printf("  %s %-22s %-8d min:%dms max:%dms avg:%dms p50:%dms p95:%dms p99:%dms\n",
						statusIcon, testName, testCount,
						stats.Min.Milliseconds(), stats.Max.Milliseconds(), stats.Mean.Milliseconds(),
						stats.P50.Milliseconds(), stats.P95.Milliseconds(), stats.P99.Milliseconds())
				}
			} else {
				fmt.Printf("  %s %-22s %-8d n/a (all tests failed)\n",
					statusIcon, testName, testCount)
			}
		}
		fmt.Printf("\n")
	}

	// Print GRPC results with integrated statistics
	if len(grpcResults) > 0 {
		fmt.Printf("⚡ GRPC RESULTS:\n")
		fmt.Printf("  %-25s %-8s %-s\n", "Test", "Tests", "Latency Statistics")
		fmt.Printf("  %s\n", strings.Repeat("-", 80))

		// Group GRPC results by test type
		grpcTestGroups := make(map[string][]BenchmarkResult)
		for _, result := range grpcResults {
			testName := strings.Replace(result.Test, "-GRPC", "", 1)
			grpcTestGroups[testName] = append(grpcTestGroups[testName], result)
		}

		// Print each test group with full statistics
		for testName, testResults := range grpcTestGroups {
			// Collect latencies from successful tests
			latencies := make([]time.Duration, 0)
			statusIcon := "✅"
			testCount := len(testResults)

			// Check if any result has iteration metadata
			for _, r := range testResults {
				if r.Details != nil {
					if detailsMap, ok := r.Details.(map[string]interface{}); ok {
						if iterations, ok := detailsMap["show_iterations"].(int); ok {
							testCount = iterations
							break
						}
					}
				}
			}

			for _, r := range testResults {
				if r.Latency > 0 && r.Status != "FAIL" {
					latencies = append(latencies, r.Latency)
				}
				if r.Status == "FAIL" {
					statusIcon = "❌"
				} else if r.Status == "WARN" && statusIcon != "❌" {
					statusIcon = "⚠️"
				}
			}

			if len(latencies) > 0 {
				if len(latencies) == 1 {
					// Check if this is aggregated statistics from multiple iterations
					if testCount > 1 {
						// This is aggregated data from multiple iterations, use the metadata stats
						if len(testResults) > 0 && testResults[0].Details != nil {
							if detailsMap, ok := testResults[0].Details.(map[string]interface{}); ok {
								if statsData, ok := detailsMap["stats"].(LatencyStats); ok {
									fmt.Printf("  %s %-22s %-8d min:%dms max:%dms avg:%dms p50:%dms p95:%dms p99:%dms\n",
										statusIcon, testName, testCount,
										statsData.Min.Milliseconds(), statsData.Max.Milliseconds(), statsData.Mean.Milliseconds(),
										statsData.P50.Milliseconds(), statsData.P95.Milliseconds(), statsData.P99.Milliseconds())
								} else {
									// Fallback to single value
									ms := latencies[0].Milliseconds()
									fmt.Printf("  %s %-22s %-8d min:%dms max:%dms avg:%dms p50:%dms p95:%dms p99:%dms\n",
										statusIcon, testName, testCount, ms, ms, ms, ms, ms, ms)
								}
							}
						}
					} else {
						// Single test - show all stats as same value
						ms := latencies[0].Milliseconds()
						fmt.Printf("  %s %-22s %-8d min:%dms max:%dms avg:%dms p50:%dms p95:%dms p99:%dms\n",
							statusIcon, testName, testCount, ms, ms, ms, ms, ms, ms)
					}
				} else {
					// Multiple tests - show real statistics
					stats := calculateLatencyStats(latencies)
					fmt.Printf("  %s %-22s %-8d min:%dms max:%dms avg:%dms p50:%dms p95:%dms p99:%dms\n",
						statusIcon, testName, testCount,
						stats.Min.Milliseconds(), stats.Max.Milliseconds(), stats.Mean.Milliseconds(),
						stats.P50.Milliseconds(), stats.P95.Milliseconds(), stats.P99.Milliseconds())
				}
			} else {
				fmt.Printf("  %s %-22s %-8d n/a (all tests failed)\n",
					statusIcon, testName, testCount)
			}
		}
		fmt.Printf("\n")
	}

	// Print WebSocket results with integrated statistics
	if len(wsResults) > 0 {
		fmt.Printf("🔌 WEBSOCKET RESULTS:\n")
		fmt.Printf("  %-25s %-8s %-s\n", "Test", "Tests", "Latency Statistics")
		fmt.Printf("  %s\n", strings.Repeat("-", 80))

		// Group WebSocket results by test type
		wsTestGroups := make(map[string][]BenchmarkResult)
		for _, result := range wsResults {
			testName := strings.Replace(result.Test, " Connection", "", 1)
			wsTestGroups[testName] = append(wsTestGroups[testName], result)
		}

		// Print each test group with full statistics
		for testName, testResults := range wsTestGroups {
			// Collect latencies from successful tests
			latencies := make([]time.Duration, 0)
			statusIcon := "✅"
			testCount := len(testResults)

			for _, r := range testResults {
				if r.Latency > 0 && r.Status != "FAIL" {
					latencies = append(latencies, r.Latency)
				}
				if r.Status == "FAIL" {
					statusIcon = "❌"
				} else if r.Status == "WARN" && statusIcon != "❌" {
					statusIcon = "⚠️"
				}
			}

			// Get testCount from metadata if available
			for _, r := range testResults {
				if r.Details != nil {
					if detailsMap, ok := r.Details.(map[string]interface{}); ok {
						if iterations, ok := detailsMap["show_iterations"].(int); ok {
							testCount = iterations
							break
						}
					}
				}
			}

			if len(latencies) > 0 {
				if len(latencies) == 1 {
					// Single test result
					ms := latencies[0].Milliseconds()
					fmt.Printf("  %s %-22s %-8d min:%dms max:%dms avg:%dms p50:%dms p95:%dms p99:%dms\n",
						statusIcon, testName, testCount, ms, ms, ms, ms, ms, ms)
				} else {
					// Multiple test results - calculate real statistics
					stats := calculateLatencyStats(latencies)
					fmt.Printf("  %s %-22s %-8d min:%dms max:%dms avg:%dms p50:%dms p95:%dms p99:%dms\n",
						statusIcon, testName, testCount,
						stats.Min.Milliseconds(), stats.Max.Milliseconds(), stats.Mean.Milliseconds(),
						stats.P50.Milliseconds(), stats.P95.Milliseconds(), stats.P99.Milliseconds())
				}
			} else {
				// No successful results
				fmt.Printf("  %s %-22s %-8d n/a (all tests failed)\n",
					statusIcon, testName, testCount)
				fmt.Printf("  %25s %8s \n", "", "")
			}
		}
		fmt.Printf("\n")
	}
}

// getStatusIcon returns the appropriate icon for a test status
func getStatusIcon(status string) string {
	switch status {
	case "PASS":
		return "✅"
	case "WARN":
		return "🟡"
	case "FAIL":
		return "❌"
	default:
		return "❓"
	}
}

func main() {
	// Check if config.yaml exists
	if _, err := os.Stat("config.yaml"); os.IsNotExist(err) {
		fmt.Printf("❌ Error: config.yaml not found\n")
		fmt.Printf("Please copy config.example.yaml to config.yaml and customize as needed\n")
		fmt.Printf("Or create config.yaml with your chain configurations\n\n")
		os.Exit(1)
	}

	// Load configuration from config.yaml
	if err := loadConfig(); err != nil {
		fmt.Printf("❌ Error: Invalid config.yaml: %v\n", err)
		fmt.Printf("Please check your config.yaml file for syntax errors\n\n")
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		fmt.Printf("🔬 BryanLabs Node Benchmark Tool\n")
		fmt.Printf("Professional blockchain infrastructure validation\n\n")
		fmt.Printf("This tool validates node configuration and performance across all protocols:\n")
		fmt.Printf("• Block discovery with actual transaction data\n")
		fmt.Printf("• Data retention capability (RPC)\n")
		fmt.Printf("• Transaction queries (RPC, REST API, GRPC)\n")
		fmt.Printf("• Event-based transaction queries (RPC, REST API, GRPC)\n")
		fmt.Printf("• Balance queries (REST API, GRPC)\n")
		fmt.Printf("• Event indexing verification (all protocols)\n\n")
		fmt.Printf("Protocol Coverage:\n")
		fmt.Printf("• RPC: CometBFT JSON-RPC endpoints\n")
		fmt.Printf("• REST API: Cosmos SDK REST endpoints\n")
		fmt.Printf("• GRPC: Basic connectivity testing (requires cosmos-sdk protobuf for full implementation)\n\n")
		fmt.Printf("Usage:\n")
		fmt.Printf("  go run benchmark.go <chain>    # Test specific chain from config.yaml\n")
		fmt.Printf("  go run benchmark.go all        # Test all enabled chains\n")
		fmt.Printf("  go run benchmark.go --json     # Output JSON format\n\n")
		availableChains := getAvailableChains()
		if len(availableChains) > 0 {
			fmt.Printf("Available chains: %s\n\n", strings.Join(availableChains, ", "))
		} else {
			fmt.Printf("No chains configured. Please check config.yaml\n\n")
		}
		fmt.Printf("🚀 Powered by BryanLabs - Professional blockchain infrastructure\n")
		return
	}

	arg := strings.ToLower(os.Args[1])
	jsonOutput := false

	// Check for help flags
	if arg == "--help" || arg == "-h" || arg == "help" {
		fmt.Printf("🔬 BryanLabs Node Benchmark Tool\n")
		fmt.Printf("Professional blockchain infrastructure validation\n\n")
		fmt.Printf("This tool validates node configuration and performance across all protocols:\n")
		fmt.Printf("• Block discovery with actual transaction data\n")
		fmt.Printf("• Data retention capability (RPC)\n")
		fmt.Printf("• Transaction queries (RPC, REST API, GRPC)\n")
		fmt.Printf("• Event-based transaction queries (RPC, REST API, GRPC)\n")
		fmt.Printf("• Balance queries (REST API, GRPC)\n")
		fmt.Printf("• Event indexing verification (all protocols)\n\n")
		fmt.Printf("Protocol Coverage:\n")
		fmt.Printf("• RPC: CometBFT JSON-RPC endpoints\n")
		fmt.Printf("• REST API: Cosmos SDK REST endpoints\n")
		fmt.Printf("• GRPC: Basic connectivity testing\n\n")
		fmt.Printf("Usage:\n")
		fmt.Printf("  go run benchmark.go <chain>    # Test specific chain from config.yaml\n")
		fmt.Printf("  go run benchmark.go all        # Test all enabled chains\n")
		fmt.Printf("  go run benchmark.go --json     # Output JSON format\n")
		fmt.Printf("  go run benchmark.go --help     # Show this help\n\n")
		availableChains := getAvailableChains()
		if len(availableChains) > 0 {
			fmt.Printf("Available chains: %s\n\n", strings.Join(availableChains, ", "))
		} else {
			fmt.Printf("No chains configured. Please check config.yaml\n\n")
		}
		fmt.Printf("🚀 Powered by BryanLabs - Professional blockchain infrastructure\n")
		return
	}

	// Check for JSON output flag
	for _, arg := range os.Args[1:] {
		if arg == "--json" {
			jsonOutput = true
			break
		}
	}

	availableChains := getAvailableChains()
	if len(availableChains) == 0 {
		fmt.Printf("❌ Error: No enabled chains found in config.yaml\n")
		fmt.Printf("Please ensure at least one chain is configured with enabled: true\n\n")
		os.Exit(1)
	}

	switch arg {
	case "all":
		for _, chain := range availableChains {
			runBenchmarks(chain)
		}
	case "--json":
		// Run all chains in JSON mode
		for _, chain := range availableChains {
			runBenchmarks(chain)
		}
	default:
		// Check if the specified chain exists in config
		found := false
		for _, chain := range availableChains {
			if chain == arg {
				found = true
				break
			}
		}
		if found {
			runBenchmarks(arg)
		} else {
			fmt.Printf("❌ Unknown chain: %s\n", arg)
			fmt.Printf("Available chains: %s, all\n", strings.Join(availableChains, ", "))
			return
		}
	}

	if jsonOutput {
		// Output results in JSON format
		jsonData, _ := json.MarshalIndent(results, "", "  ")
		fmt.Printf("\n%s\n", string(jsonData))
	} else {
		generateReport()
	}
}
