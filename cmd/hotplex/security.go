package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hotplex/hotplex-worker/internal/cli"
	"github.com/hotplex/hotplex-worker/internal/cli/checkers"
	"github.com/hotplex/hotplex-worker/internal/config"
)

func newSecurityCmd() *cobra.Command {
	var fix, verbose, jsonOutput bool

	cmd := &cobra.Command{
		Use:   "security",
		Short: "Run security audit",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config")
			if configPath == "" {
				configPath = "~/.hotplex/config.yaml"
			}
			checkers.SetConfigPath(configPath)

			checkersToRun := cli.DefaultRegistry.ByCategory("security")

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			var diags []cli.Diagnostic
			for _, c := range checkersToRun {
				d := c.Check(ctx)
				diags = append(diags, d)
			}

			diags = append(diags, checkTLSConfig(ctx, configPath))
			diags = append(diags, checkSSRFConfig(ctx, configPath))

			if fix {
				fixed, fixFailed := 0, 0
				for i, d := range diags {
					if d.Status != cli.StatusPass && d.FixFunc != nil {
						if err := d.FixFunc(); err != nil {
							diags[i].Message = fmt.Sprintf("%s (fix failed: %s)", d.Message, err)
							fixFailed++
						} else {
							if i < len(checkersToRun) {
								recheck := checkersToRun[i].Check(ctx)
								diags[i] = recheck
							}
							fixed++
						}
					}
				}
				if fixFailed > 0 {
					outputResults(os.Stderr, diags, verbose, jsonOutput)
					fmt.Fprintf(os.Stderr, "\n%d fix(es) applied, %d failed\n", fixed, fixFailed)
					os.Exit(3)
				}
				if fixed > 0 {
					fmt.Fprintf(os.Stderr, "%d fix(es) applied successfully\n", fixed)
				}
			}

			outputResults(os.Stderr, diags, verbose, jsonOutput)

			_, _, fail := countStatuses(diags)
			if fail > 0 {
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&fix, "fix", false, "automatically fix issues")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show detailed information")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	cmd.Flags().StringP("config", "c", "~/.hotplex/config.yaml", "config file path")
	return cmd
}

// checkTLSConfig warns if TLS is disabled on a non-local address.
func checkTLSConfig(_ context.Context, cfgPath string) cli.Diagnostic {
	const name = "security.tls_config"
	const cat = "security"

	if cfgPath == "" {
		return cli.Diagnostic{
			Name:     name,
			Category: cat,
			Status:   cli.StatusWarn,
			Message:  "Cannot check TLS config (no config path)",
		}
	}

	cfg, err := config.Load(cfgPath, config.LoadOptions{})
	if err != nil {
		return cli.Diagnostic{
			Name:     name,
			Category: cat,
			Status:   cli.StatusWarn,
			Message:  "Cannot load config for TLS check",
			Detail:   err.Error(),
		}
	}

	if cfg.Security.TLSEnabled {
		return cli.Diagnostic{
			Name:     name,
			Category: cat,
			Status:   cli.StatusPass,
			Message:  "TLS is enabled",
		}
	}

	addr := cfg.Gateway.Addr
	if isLocalAddr(addr) {
		return cli.Diagnostic{
			Name:     name,
			Category: cat,
			Status:   cli.StatusPass,
			Message:  "TLS not required for local address",
			Detail:   addr,
		}
	}

	return cli.Diagnostic{
		Name:     name,
		Category: cat,
		Status:   cli.StatusWarn,
		Message:  "TLS is disabled on non-local gateway address",
		Detail:   fmt.Sprintf("gateway.addr=%s — traffic is unencrypted", addr),
		FixHint:  "Set security.tls_enabled=true and provide tls_cert_file + tls_key_file",
	}
}

// checkSSRFConfig warns if allowed_origins contains wildcard.
func checkSSRFConfig(_ context.Context, cfgPath string) cli.Diagnostic {
	const name = "security.ssrf_origins"
	const cat = "security"

	if cfgPath == "" {
		return cli.Diagnostic{
			Name:     name,
			Category: cat,
			Status:   cli.StatusWarn,
			Message:  "Cannot check SSRF config (no config path)",
		}
	}

	cfg, err := config.Load(cfgPath, config.LoadOptions{})
	if err != nil {
		return cli.Diagnostic{
			Name:     name,
			Category: cat,
			Status:   cli.StatusWarn,
			Message:  "Cannot load config for SSRF check",
			Detail:   err.Error(),
		}
	}

	for _, o := range cfg.Security.AllowedOrigins {
		if o == "*" {
			return cli.Diagnostic{
				Name:     name,
				Category: cat,
				Status:   cli.StatusWarn,
				Message:  "Allowed origins set to wildcard (SSRF risk)",
				Detail:   "security.allowed_origins contains \"*\" — any origin can connect",
				FixHint:  "Restrict allowed_origins to specific domains",
			}
		}
	}

	return cli.Diagnostic{
		Name:     name,
		Category: cat,
		Status:   cli.StatusPass,
		Message:  "Allowed origins properly restricted",
	}
}

// isLocalAddr returns true for localhost/127.0.0.1/::1 or empty address.
func isLocalAddr(addr string) bool {
	host := addr
	if h, _, err := splitHostPort(addr); err == nil {
		host = h
	}
	return host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func splitHostPort(addr string) (string, string, error) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return addr, "", fmt.Errorf("no port")
	}
	return addr[:i], addr[i+1:], nil
}
