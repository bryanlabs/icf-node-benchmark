# ICF Node Testing Tools - Makefile

.PHONY: build build-benchmark build-stress clean benchmark stress install help

# Default target
build: build-benchmark build-stress ## Build both benchmark and stress binaries

build-benchmark: ## Build the benchmark binary
	@echo "🔨 Building benchmark tool..."
	go build -o bin/benchmark ./cmd/benchmark
	@echo "✅ Build complete: ./bin/benchmark"

build-stress: ## Build the stress test binary
	@echo "🔨 Building stress test tool..."
	go build -o bin/stress ./cmd/stress
	@echo "✅ Build complete: ./bin/stress"

install: ## Install dependencies
	@echo "📦 Installing dependencies..."
	go mod download
	go mod tidy
	@echo "✅ Dependencies installed"

clean: ## Clean built binaries
	@echo "🧹 Cleaning binaries..."
	rm -rf bin/
	@echo "✅ Clean complete"

benchmark: build-benchmark ## Build and run benchmark (use ARGS='neutron' for specific chain)
	./bin/benchmark $(ARGS)

stress: build-stress ## Build and run stress test (use ARGS='neutron 100 60' for specific params)
	./bin/stress $(ARGS)

help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Examples:"
	@echo "  make benchmark ARGS=neutron"
	@echo "  make stress ARGS='neutron 100 60'"
	@echo "  make stress ARGS='all 50 30'"
