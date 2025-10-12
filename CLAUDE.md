# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is provider-http, a Crossplane Provider that enables managing resources through HTTP requests. The provider allows users to create, update, and delete resources by sending HTTP requests to external APIs, making it useful for integrating with REST APIs that don't have dedicated Crossplane providers.

## Key Resources

The provider implements two main custom resources:
- **Request**: Manages persistent resources through HTTP CRUD operations with full lifecycle management
- **DisposableRequest**: Executes one-time HTTP requests without ongoing resource management

## Development Commands

### Building and Testing
```bash
# Build the provider
make build

# Run all tests  
make test

# Run end-to-end tests
make e2e

# Run local development against cluster
make run

# Generate code (CRDs, deep copy methods, etc.)
make generate

# Local development with deployed provider
make local-dev        # Start controlplane
make local-deploy     # Build and deploy provider locally
```

### Code Quality
```bash
# The project uses golangci-lint for linting
# Lint configuration is defined in the build submodule

# Check Go module dependencies
go mod verify
go mod tidy
```

## Architecture

### Controller Structure
- **Main entry point**: `cmd/provider/main.go` - Sets up the controller manager with rate limiting, leader election, and timeout configuration
- **Controller setup**: `internal/controller/http.go` - Orchestrates all controllers (config, request, disposablerequest)
- **Core controllers**:
  - `internal/controller/request/` - Handles Request resource lifecycle
  - `internal/controller/disposablerequest/` - Handles DisposableRequest lifecycle  
  - `internal/controller/config/` - Manages provider configuration

### Request Controller Components
The Request controller is the most complex, with several specialized modules:
- **observe/** - Health checks and synchronization validation (deleted, synced, jq-based checks)
- **requestgen/** - Generates HTTP requests from resource specifications
- **requestmapping/** - Maps Crossplane actions (CREATE/UPDATE/DELETE) to HTTP methods
- **requestprocessing/** - Executes HTTP requests with retry logic and timeout handling
- **responseconverter/** - Converts HTTP responses to Kubernetes resource status
- **statushandler/** - Updates Crossplane resource status based on HTTP responses

### Key Utilities
- **data-patcher/** - Handles secret injection and data transformation for request payloads
- **jq/** - JQ expression parsing and evaluation for response processing
- **kube-handler/** - Kubernetes client operations and secret management
- **utils/** - Common utilities for validation, string manipulation, and status updates

### API Versions
The project maintains v1alpha1 and v1alpha2 APIs with the following progression:
- v1alpha1: Legacy API version
- v1alpha2: Current stable API version with enhanced features
- Generated files (zz_generated.*) are automatically created by controller-tools

### HTTP Client
- **internal/clients/http/** - Centralized HTTP client with timeout, TLS, and authentication support

### Namespaced vs Non-Namespaced Resources

**CRITICAL**: The namespaced and non-namespaced resources MUST be functionally equivalent:

- **NamespacedRequest** and **Request** must have identical functionality except for scope (namespace vs cluster)
- **NamespacedDisposableRequest** and **DisposableRequest** must have identical functionality except for scope
- Both versions must support the same:
  - HTTP CRUD operations and lifecycle management
  - Secret injection and data transformation capabilities  
  - Response processing and JQ filtering
  - Status handling and error management
  - Test coverage and validation

When making changes to one resource type, always ensure the corresponding namespaced/non-namespaced version receives identical updates. This ensures users can choose between namespace-scoped and cluster-scoped resources based on their access control needs without sacrificing functionality.

## Testing Strategy

Tests are co-located with source files using Go conventions:
- Unit tests: `*_test.go` files alongside source code
- End-to-end tests: Uses uptest framework with examples in `examples/sample/`
- Integration tests: Validate controller behavior against real Kubernetes clusters

## Build System

Uses Make with Crossplane's standard build submodule:
- Build configuration in `Makefile` references `build/makelib/*.mk`
- Supports multi-platform builds (linux_amd64, linux_arm64)
- Container image building and Crossplane package (xpkg) creation
- Code generation for CRDs and Go types

## Key Configuration
- Go version: 1.24+ (defined in go.mod)
- Crossplane runtime: v2.0.0
- Uses controller-runtime for Kubernetes controller implementation
- JQ library integration for response processing: github.com/itchyny/gojq