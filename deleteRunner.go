package main

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func deleteRunner(clientset *kubernetes.Clientset, name string) {
	deletePropagation := metav1.DeletePropagationBackground

	err := clientset.BatchV1().Jobs("default").Delete(context.TODO(), name,
		metav1.DeleteOptions{PropagationPolicy: &deletePropagation})
	if err != nil {
		panic(err)
	}

	err = clientset.CoreV1().Services("default").Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		panic(err.Error())
	}
}
