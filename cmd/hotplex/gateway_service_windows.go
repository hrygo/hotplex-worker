//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/svc"

	"github.com/hrygo/hotplex/internal/service"
)

func isServiceRun() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--service-run" {
			return true
		}
	}
	return false
}

func extractServiceConfig() string {
	args := os.Args[1:]
	for i, arg := range args {
		if arg == "--service-config" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "--service-config=") {
			return strings.TrimPrefix(arg, "--service-config=")
		}
	}
	return ""
}

type gatewayService struct {
	configPath string
	stopCh     chan struct{}
}

func runAsWindowsService(configPath string) {
	s := &gatewayService{
		configPath: configPath,
		stopCh:     make(chan struct{}),
	}
	if err := svc.Run("hotplex", s); err != nil {
		fmt.Fprintf(os.Stderr, "service: %v\n", err)
		os.Exit(1)
	}
}

func (s *gatewayService) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}

	logDir := service.LogDir(service.LevelUser)
	_ = os.MkdirAll(logDir, 0o755)
	logFile, err := os.OpenFile(filepath.Join(logDir, "service.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		os.Stdout = logFile
		os.Stderr = logFile
	}

	var runErr error
	done := make(chan struct{})
	go func() {
		runErr = runGateway(s.configPath, false, s.stopCh)
		close(done)
	}()

	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				close(s.stopCh)
				<-done
				if logFile != nil {
					_ = logFile.Close()
				}
				return false, 0
			}
		case <-done:
			if logFile != nil {
				_ = logFile.Close()
			}
			if runErr != nil {
				return true, 1
			}
			return false, 0
		}
	}
}
