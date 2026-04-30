package main

import "syscall"

func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
