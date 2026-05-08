# HotPlex Java Client - Makefile

.PHONY: build test clean run help

# Build the project
build:
	mvn clean compile

# Run tests
test:
	mvn test

# Clean build artifacts
clean:
	mvn clean

# Run the QuickStart example
# Note: requires HOTPLEX_SIGNING_KEY to be set
run:
	mvn compile exec:java -Dexec.mainClass="dev.hotplex.example.QuickStart"

# Run the Interactive example
# Note: requires HOTPLEX_SIGNING_KEY to be set
interactive:
	mvn compile exec:java -Dexec.mainClass="dev.hotplex.example.InteractiveExample"

# Install locally
install:
	mvn clean install -DskipTests

# Help message
help:
	@echo "HotPlex Java Client - Build & Run Commands"
	@echo "=========================================="
	@echo "  make build       - Compile the project"
	@echo "  make test        - Run unit tests"
	@echo "  make clean       - Remove build artifacts"
	@echo "  make run         - Run QuickStart example (requires HOTPLEX_SIGNING_KEY)"
	@echo "  make interactive - Run Interactive example (requires HOTPLEX_SIGNING_KEY)"
	@echo "  make install     - Install to local Maven repository"
	@echo "  make help        - Show this help message"
