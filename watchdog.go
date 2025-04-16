package main

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	ppid := os.Getppid()

	for {
		err := syscall.Kill(ppid, 0)
		if err != nil {
			kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")

			config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
			if err != nil {
				panic(err)
			}

			clientset, err := kubernetes.NewForConfig(config)
			if err != nil {
				panic(err)
			}

			err = clientset.BatchV1().Jobs("default").Delete(context.TODO(), "ollama-runner", metav1.DeleteOptions{})
			err = clientset.CoreV1().Services("default").Delete(context.TODO(), "ollama-runner", metav1.DeleteOptions{})

			os.Exit(1)
		}
		time.Sleep(time.Minute)
	}
}
