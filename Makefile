# ICF Node Benchmark Tool - Makefile

.PHONY: build install

build: ## Build the benchmark binary
	@echo "🔨 Building benchmark tool..."
	go build -o benchmark .
	@echo "✅ Build complete: ./benchmark"

install: ## Install dependencies
	@echo "📦 Installing dependencies..."
	go mod download
	go mod tidy
	@echo "✅ Dependencies installed"
