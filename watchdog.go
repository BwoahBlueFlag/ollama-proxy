package main

import (
	"k8s.io/client-go/rest"
	"os"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
)

func main() {
	ppid := os.Getppid()
	name := os.Args[1]

	for {
		err := syscall.Kill(ppid, 0)
		if err != nil {
			config, err := rest.InClusterConfig()
			if err != nil {
				panic(err.Error())
			}

			clientset, err := kubernetes.NewForConfig(config)
			if err != nil {
				panic(err.Error())
			}

			deleteRunner(clientset, name)

			os.Exit(1)
		}
		time.Sleep(time.Minute)
	}
}
