package main

import (
	"context"
	"k8s.io/client-go/rest"
	"os"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func main() {
	ppid := os.Getppid()

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

			err = clientset.BatchV1().Jobs("default").Delete(context.TODO(), "ollama-runner", metav1.DeleteOptions{})
			err = clientset.CoreV1().Services("default").Delete(context.TODO(), "ollama-runner", metav1.DeleteOptions{})

			os.Exit(1)
		}
		time.Sleep(time.Minute)
	}
}
