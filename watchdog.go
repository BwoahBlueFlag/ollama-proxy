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
			cmd := exec.Command("kubectl", "delete", "job", "ollama-runner")
			err = cmd.Start()
			err = cmd.Wait()

			cmd = exec.Command("kubectl", "delete", "service", "ollama-runner")
			err = cmd.Start()
			err = cmd.Wait()

			os.Exit(1)
		}
		time.Sleep(time.Minute)
	}
}
