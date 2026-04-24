// 08_token_generator — Standalone JWT token generation utility.
//
// Generates ES256 JWT tokens for HotPlex Gateway authentication.
// Supports PEM file, 64-char hex, or 44-char base64 key formats.
//
// Usage:
//
//	HOTPLEX_SIGNING_KEY=<key> go run ./08_token_generator
//	HOTPLEX_SIGNING_KEY=<key> go run ./08_token_generator -sub user-123 -scopes admin,write -ttl 2h -v
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	client "github.com/hrygo/hotplex/client"
)

func main() {
	subject := flag.String("sub", "example-user", "JWT subject (user ID)")
	scopes := flag.String("scopes", "read,write", "Comma-separated scopes")
	ttl := flag.Duration("ttl", 1*time.Hour, "Token TTL (e.g. 1h, 30m, 24h)")
	audience := flag.String("aud", "gateway", "JWT audience")
	showClaims := flag.Bool("v", false, "Print decoded claims")
	flag.Parse()

	keyStr := os.Getenv("HOTPLEX_SIGNING_KEY")
	if keyStr == "" && flag.NArg() > 0 {
		keyStr = flag.Arg(0)
	}
	if keyStr == "" {
		fmt.Fprintln(os.Stderr, "Usage: HOTPLEX_SIGNING_KEY=<key> go run ./08_token_generator [flags]")
		fmt.Fprintln(os.Stderr, "\nKey formats: PEM file path, 64-char hex, or 44-char base64")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		flag.PrintDefaults()
		os.Exit(1) //nolint:gocritic // example exit
	}

	gen, err := client.NewTokenGenerator(keyStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load key: %v\n", err)
		os.Exit(1) //nolint:gocritic // example exit
	}

	token, err := gen.WithAudience(*audience).Generate(*subject, strings.Split(*scopes, ","), *ttl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate token: %v\n", err)
		os.Exit(1) //nolint:gocritic // example exit
	}

	fmt.Println(token)

	if *showClaims {
		fmt.Println()
		fmt.Println("==================================================")
		fmt.Println("  Token Info")
		fmt.Println("==================================================")
		fmt.Printf("Subject:  %s\n", *subject)
		fmt.Printf("Scopes:   %s\n", *scopes)
		fmt.Printf("Audience: %s\n", *audience)
		fmt.Printf("TTL:      %s\n", *ttl)
		fmt.Printf("Expires:  %s\n", time.Now().Add(*ttl).Format(time.RFC3339))
	}
}
