package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var replaceRunnerMutex sync.Mutex
var runnerMutex sync.Mutex
var runner *Runner
var runnerIndex int
var clientset *kubernetes.Clientset
var args []string

type Runner struct {
	name           string
	watchdog       *exec.Cmd
	activeRequests atomic.Int32
}

func main() {
	currentTime := time.Now()
	fileName := "logs/" + currentTime.Format("2006-01-02_15-04-05") + ".txt"

	os.Mkdir("logs", 0777)
	file, err := os.Create(fileName)
	if err != nil {
		return
	}
	defer file.Close()

	args = os.Args

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

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	runner = startRunner(clientset, runnerIndex, args...)

	addr := "127.0.0.1:" + proxyPort
	go func() {
		defer wg.Done()
		mux := http.NewServeMux()
		mux.HandleFunc("/replace", handleReplace)
		mux.HandleFunc("/", handle)
		err = http.ListenAndServe(addr, mux)
	}()

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

func getRunner() *Runner {
	runnerMutex.Lock()
	defer runnerMutex.Unlock()
	runner.activeRequests.Add(1)
	return runner
}

func handle(w http.ResponseWriter, r *http.Request) {
	requestRunner := getRunner()
	defer requestRunner.activeRequests.Add(-1)

	targetURL := "http://" + requestRunner.name + ":57156" + r.URL.Path

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

func handleReplace(w http.ResponseWriter, r *http.Request) {
	replaceAndDeleteRunner()
}

func startRunner(clientset *kubernetes.Clientset, index int, args ...string) *Runner {
	name := "ollama-runner" + strconv.Itoa(index)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    name,
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

	_, err := clientset.BatchV1().Jobs("default").Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": name,
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

	cmd := exec.Command("./ollama-proxy-watchdog", name)
	err = cmd.Start()
	if err != nil {
		panic(err)
	}

	return &Runner{name: name, watchdog: cmd}
}

func replaceAndDeleteRunner() {
	if !replaceRunnerMutex.TryLock() {
		return
	}
	defer replaceRunnerMutex.Unlock()

	runnerIndex++
	newRunner := startRunner(clientset, runnerIndex, args...)
	err := WaitUntilRunning(newRunner.name)
	if err != nil {
		panic(err)
	}

	oldRunner := replaceRunner(newRunner)
	for oldRunner.activeRequests.Load() > 0 {
		time.Sleep(time.Second)
	}
	_ = oldRunner.watchdog.Cancel()
	deleteRunner(clientset, oldRunner.name)
}

func replaceRunner(newRunner *Runner) *Runner {
	runnerMutex.Lock()
	defer runnerMutex.Unlock()

	oldRunner := runner
	runner = newRunner
	return oldRunner
}

// following section is modified code from ollama v0.6.1

type ServerStatus int

const ( // iota is reset to 0
	ServerStatusReady ServerStatus = iota
	ServerStatusNoSlotsAvailable
	ServerStatusLoadingModel
	ServerStatusNotResponding
	ServerStatusError
)

func (s ServerStatus) ToString() string {
	switch s {
	case ServerStatusReady:
		return "llm server ready"
	case ServerStatusNoSlotsAvailable:
		return "llm busy - no slots available"
	case ServerStatusLoadingModel:
		return "llm server loading model"
	case ServerStatusNotResponding:
		return "llm server not responding"
	default:
		return "llm server error"
	}
}

type ServerStatusResp struct {
	Status          string  `json:"status"`
	SlotsIdle       int     `json:"slots_idle"`
	SlotsProcessing int     `json:"slots_processing"`
	Error           string  `json:"error"`
	Progress        float32 `json:"progress"`
}

func getRunnerStatus(name string) (ServerStatus, error) {
	req, err := http.NewRequest(http.MethodGet, "http://"+name+":57156/health", nil)
	if err != nil {
		return ServerStatusError, fmt.Errorf("error creating GET request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ServerStatusError, fmt.Errorf("health resp: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ServerStatusError, fmt.Errorf("read health request: %w", err)
	}

	var status ServerStatusResp
	if err = json.Unmarshal(body, &status); err != nil {
		return ServerStatusError, fmt.Errorf("health unmarshal encode response: %w", err)
	}

	switch status.Status {
	case "ok":
		return ServerStatusReady, nil
	case "no slot available":
		return ServerStatusNoSlotsAvailable, nil
	case "loading model":
		return ServerStatusLoadingModel, nil
	default:
		return ServerStatusError, fmt.Errorf("server error: %+v", status)
	}
}

func WaitUntilRunning(name string) error {
	stallDuration := 60 * time.Minute           // If no progress happens
	stallTimer := time.Now().Add(stallDuration) // give up if we stall

	for {
		if time.Now().After(stallTimer) {
			// timeout
			return fmt.Errorf("timed out waiting for llama runner to start")
		}
		status, _ := getRunnerStatus(name)
		switch status {
		case ServerStatusReady:
			return nil
		default:
			time.Sleep(time.Millisecond * 250)
			continue
		}
	}
}
