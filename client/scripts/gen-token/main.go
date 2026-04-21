//go:build ignore

// gen-token generates ES256 JWT tokens for HotPlex Gateway authentication.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	client "github.com/hotplex/hotplex-go-client"
)

var (
	flagSubject  = flag.String("sub", "example-user", "JWT subject (user ID)")
	flagScopes   = flag.String("scopes", "read,write", "comma-separated scopes")
	flagTTL      = flag.Duration("ttl", 1*time.Hour, "token TTL (e.g. 1h, 30m)")
	flagAudience = flag.String("aud", "gateway", "JWT audience")
	flagBotID    = flag.String("bot-id", "", "Bot ID for isolation (SEC-007)")
)

func main() {
	flag.Parse()

	keyStr := os.Getenv("HOTPLEX_SIGNING_KEY")
	if keyStr == "" && flag.NArg() > 0 {
		keyStr = flag.Arg(0)
	}
	if keyStr == "" {
		fmt.Fprintln(os.Stderr, "Usage: HOTPLEX_SIGNING_KEY=<key> go run gen-token/main.go")
		fmt.Fprintln(os.Stderr, "  or:  go run gen-token/main.go /path/to/key.pem")
		fmt.Fprintln(os.Stderr, "Key formats: PEM file, 64-char hex, or 44-char base64")
		os.Exit(1)
	}

	gen, err := client.NewTokenGenerator(keyStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create token generator: %v\n", err)
		os.Exit(1)
	}

	_ = gen.WithAudience(*flagAudience)
	if *flagBotID != "" {
		_ = gen.WithBotID(*flagBotID)
	}

	token, err := gen.Generate(*flagSubject, strings.Split(*flagScopes, ","), *flagTTL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate token: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(token)
}
