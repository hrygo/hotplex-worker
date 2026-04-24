# pkg/ - Public Packages

This directory contains reusable packages that can be shared between the HotPlex Worker Gateway server and client implementations.

## Packages

### `pkg/events/` - AEP Event Types

Defines all AEP v1 event types and constants. Shared by both gateway and clients.

**Content:**
- `events.Kind` - Event type constants (Error, State, Input, Done, etc.)
- `events.ErrorCode` - Standardized error codes
- `events.SessionState` - Session state constants
- `events.Envelope` - The unified AEP v1 message envelope

### `pkg/aep/` - AEP Protocol Codec

Implements the AEP v1 protocol encoding/decoding logic.

**Content:**
- `aep.Encode()` / `aep.Decode()` - NDJSON stream encoding/decoding
- `aep.EncodeJSON()` / `aep.DecodeLine()` - JSON line encoding/decoding
- `aep.NewID()` / `aep.NewSessionID()` - ID generation
- `aep.NewInitEnvelope()` - Create init handshake messages
- `aep.ValidateInit()` - Validate init messages
- `aep.BuildInitAck()` / `aep.BuildInitAckError()` - Build init acknowledgment messages

**Usage:**

```go
import "hotplex/pkg/aep"

// Encode an envelope
data, err := aep.EncodeJSON(env)
if err != nil {
    // handle error
}

// Decode a line
env, err := aep.DecodeLine(data)
if err != nil {
    // handle error
}

// Create init envelope
init := aep.NewInitEnvelope(
    aep.WorkerClaudeCode,
    aep.WithAuthToken("my-token"),
    aep.WithConfig(aep.InitConfig{
        Model: "claude-sonnet-4-6",
    }),
)
```

### `pkg/jwt/` - JWT Authentication

Provides JWT token generation and validation.

**Content:**
- `jwt.Generator` - Generate JWT tokens with ES256 signing
- `jwt.Validator` - Validate JWT tokens
- `jwt.Claims` - HotPlex-specific JWT claims
- `jwt.NewJTI()` - Generate unique JWT IDs

**Usage:**

```go
import "hotplex/pkg/jwt"

// Generate a token
generator := jwt.NewGenerator(privateKey, "hotplex", "gateway")
claims := &jwt.Claims{
    UserID: "user123",
    Scopes: []string{"worker:use"},
}
token, err := generator.Generate(claims, jwt.DefaultTTL(jwt.TokenTypeAccess))

// Validate a token
validator := jwt.NewValidator(privateKey, "gateway")
claims, err := validator.Validate(token)
if err != nil {
    // handle error
}
```

## Module Boundaries

```
pkg/                          # Public packages (stable API)
├── events/                   # Event types only
├── aep/                      # Protocol codec (imports events)
└── jwt/                      # JWT auth (standalone)

internal/                     # Internal packages (gateway-specific)
├── aep/                      # Gateway-specific codec (imports pkg/aep)
├── gateway/                  # Gateway implementation
├── security/                 # Security middleware (imports pkg/jwt)
└── worker/                  # Worker implementations
```

## Stability Guarantee

Packages in `pkg/` follow semantic versioning and maintain backward compatibility within a major version. Breaking changes will increment the major version.
