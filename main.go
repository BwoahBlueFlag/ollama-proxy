package main

import (
	"context"
	"encoding/json"
	"io"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var activeRequests atomic.Int32

func main() {
	currentTime := time.Now()
	fileName := "logs/" + currentTime.Format("2006-01-02_15-04-05") + ".txt"

	os.Mkdir("logs", 0777)
	file, err := os.Create(fileName)
	if err != nil {
		return
	}
	defer file.Close()

	args := os.Args

	_, err = file.WriteString(strings.Join(args, " ") + "\n")
	if err != nil {
		return
	}

	portIndex := getPortIndex(args)
	proxyPort := "8080"
	if portIndex > 0 {
		proxyPort = args[portIndex]
	}

	if portIndex > 0 {
		args[portIndex] = "57157"
	} else {
		args = append(args, "--port", "57157")
	}

	var wg sync.WaitGroup
	wg.Add(1)

	addr := "127.0.0.1:" + proxyPort
	go func() {
		defer wg.Done()
		mux := http.NewServeMux()
		mux.HandleFunc("/busy", handleBusy)
		mux.HandleFunc("/", handle)
		err = http.ListenAndServe(addr, mux)
	}()

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ollama-runner",
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "ollama-runner",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "ollama-runner",
							Image:   "xjanci14/ollama-proxy",
							Command: []string{"./run-runner.sh"},
							Args:    args[1:],
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 57156,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "models",
									MountPath: "/mnt/models",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "models",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "models",
								},
							},
						},
					},
				},
			},
		},
	}

	_, err = clientset.BatchV1().Jobs("default").Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ollama-runner",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "ollama-runner",
			},
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       57156,
					TargetPort: intstr.FromInt32(57156),
				},
			},
		},
	}

	_, err = clientset.CoreV1().Services("default").Create(context.TODO(), service, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}

	cmd := exec.Command("./ollama-proxy-watchdog")
	cmd.Stdout = file
	cmd.Stderr = file
	err = cmd.Start()

	wg.Wait()
}

func getPortIndex(args []string) int {
	for i, arg := range args {
		if arg == "--port" && i+1 < len(args) {
			return i + 1
		}
	}
	return 0
}

func handle(w http.ResponseWriter, r *http.Request) {
	activeRequests.Add(1)
	defer activeRequests.Add(-1)

	targetURL := "http://ollama-runner:57156" + r.URL.Path

	newReq, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "Failed to create new request", http.StatusInternalServerError)
		return
	}
	newReq.Header = r.Header

	client := &http.Client{}
	resp, err := client.Do(newReq)
	if err != nil {
		http.Error(w, "Failed to forward request", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handleBusy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{
		"busy": activeRequests.Load() > 0,
	})
}
