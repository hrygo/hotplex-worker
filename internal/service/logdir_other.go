//go:build !windows

package service

func systemLogDir() string {
	return "/var/log/hotplex"
}
