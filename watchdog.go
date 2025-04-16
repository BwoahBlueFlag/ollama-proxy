package main

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func main() {
	ppid := os.Getppid()

	for {
		err := syscall.Kill(ppid, 0)
		if err != nil {
			cmd := exec.Command("kubectl", "delete", "pod", "ollama-runner")
			err = cmd.Start()
			err = cmd.Wait()
			os.Exit(1)
		}
		time.Sleep(time.Minute)
	}
}
