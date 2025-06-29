package main

import (
	"bufio"
	"fmt"
	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/discover"
	"github.com/ollama/ollama/envconfig"
	"github.com/ollama/ollama/fs/ggml"
	"github.com/ollama/ollama/llama"
	"github.com/ollama/ollama/llm"
	"github.com/ollama/ollama/model"
	"github.com/ollama/ollama/server"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func main() {
	args := os.Args

	modelIndex := getArgumentIndex(args, "--model")
	if modelIndex <= 0 {
		fmt.Println("No model specified")
		os.Exit(1)
	}
	modelPath := args[modelIndex]

	portIndex := getArgumentIndex(args, "--port")
	if portIndex <= 0 {
		fmt.Println("No port specified")
		os.Exit(1)
	}
	port := args[portIndex]

	parts := strings.SplitN(modelPath, "/blobs", 2)
	if len(parts) != 2 {
		fmt.Println("Path does not contain /blobs")
		os.Exit(1)
	}
	prefix := parts[0] // Everything before /blobs

	const tag = "sha256-"
	idx := strings.Index(parts[1], tag)
	if idx == -1 {
		fmt.Println("Path does not contain sha256-")
		os.Exit(1)
	}
	hash := parts[1][idx+len(tag):]

	fmt.Println("Prefix:", prefix)
	fmt.Println("Hash:  ", hash)

	matches, err := searchFiles(prefix+"/manifests/registry.ollama.ai/library", hash)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if len(matches) == 0 {
		fmt.Println("No matches found.")
		os.Exit(1)
	} else {
		fmt.Println("Files containing the search string:")
		for _, match := range matches {
			fmt.Println(" -", match)
		}
	}

	modelName, err := transformPath(matches[0])
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	fmt.Println(modelName)

	model, err := server.GetModel(modelName)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	opts, err := modelOptions(model /*, requestOpts*/)
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}

	var gpus discover.GpuInfoList
	if opts.NumGPU == 0 {
		gpus = discover.GetCPUInfo()
	} else {
		gpus = discover.GetGPUInfo()
	}

	ggml, err := llm.LoadModel(modelPath, 0)
	if err != nil {
		fmt.Println(err)
		os.Exit(3)
	}

	numParallel := int(envconfig.NumParallel()) // TODO further restraints

	params := getParams(gpus, modelPath, ggml, model.AdapterPaths, model.ProjectorPaths, opts, numParallel)
	if params == nil {
		os.Exit(4)
	}

	params = append(params, "--port", port)

	fmt.Println()
	fmt.Println("====")
	fmt.Println(params)

	cmd := exec.Command("/bin/ollama", params...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	if err != nil {
		panic(err)
	}
	cmd.Wait()
}

func getArgumentIndex(args []string, key string) int {
	for i, arg := range args {
		if arg == key && i+1 < len(args) {
			return i + 1
		}
	}
	return 0
}

func transformPath(path string) (string, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")

	if len(parts) < 2 {
		return "", fmt.Errorf("invalid path format")
	}

	name := parts[len(parts)-2]
	tag := parts[len(parts)-1]

	return fmt.Sprintf("%s:%s", name, tag), nil
}

func searchFiles(root, searchStr string) ([]string, error) {
	var matchedFiles []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		found, err := fileContains(path, searchStr)
		if err != nil {
			fmt.Printf("Warning: skipping %s: %v\n", path, err)
			return nil
		}

		if found {
			matchedFiles = append(matchedFiles, path)
		}

		return nil
	})

	return matchedFiles, err
}

func fileContains(path string, searchStr string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), searchStr) {
			return true, nil
		}
	}
	return false, scanner.Err()
}

func modelOptions(model *server.Model /*, requestOpts map[string]interface{}*/) (api.Options, error) {
	opts := api.DefaultOptions()
	if err := opts.FromMap(model.Options); err != nil {
		return api.Options{}, err
	}

	/*if err := opts.FromMap(requestOpts); err != nil {
		return api.Options{}, err
	}*/

	return opts, nil
}

// NewLlamaServer will run a server for the given GPUs
// The gpu list must be a single family.
func getParams(gpus discover.GpuInfoList, modelPath string, f *ggml.GGML, adapters, projectors []string, opts api.Options, numParallel int) []string {
	systemInfo := discover.GetSystemInfo()
	systemTotalMemory := systemInfo.System.TotalMemory
	systemFreeMemory := systemInfo.System.FreeMemory
	systemSwapFreeMemory := systemInfo.System.FreeSwap
	//slog.Info("system memory", "total", format.HumanBytes2(systemTotalMemory), "free", format.HumanBytes2(systemFreeMemory), "free_swap", format.HumanBytes2(systemSwapFreeMemory))

	// If the user wants zero GPU layers, reset the gpu list to be CPU/system ram info
	if opts.NumGPU == 0 {
		gpus = discover.GetCPUInfo()
	}

	estimate := llm.EstimateGPULayers(gpus, f, projectors, opts)
	if len(gpus) > 1 || gpus[0].Library != "cpu" {
		switch {
		case gpus[0].Library == "metal" && estimate.VRAMSize > systemTotalMemory:
			// disable partial offloading when model is greater than total system memory as this
			// can lead to locking up the system
			opts.NumGPU = 0
		case gpus[0].Library != "metal" && estimate.Layers == 0:
			// Don't bother loading into the GPU if no layers can fit
			gpus = discover.GetCPUInfo()
		case opts.NumGPU < 0 && estimate.Layers > 0 && gpus[0].Library != "cpu":
			opts.NumGPU = estimate.Layers
		}
	}

	// On linux and windows, over-allocating CPU memory will almost always result in an error
	// Darwin has fully dynamic swap so has no direct concept of free swap space
	if runtime.GOOS != "darwin" {
		systemMemoryRequired := estimate.TotalSize - estimate.VRAMSize
		available := systemFreeMemory + systemSwapFreeMemory
		if systemMemoryRequired > available {
			//slog.Warn("model request too large for system", "requested", format.HumanBytes2(systemMemoryRequired), "available", available, "total", format.HumanBytes2(systemTotalMemory), "free", format.HumanBytes2(systemFreeMemory), "swap", format.HumanBytes2(systemSwapFreeMemory))
			return nil //, fmt.Errorf("model requires more system memory (%s) than is available (%s)", format.HumanBytes2(systemMemoryRequired), format.HumanBytes2(available))
		}
	}

	//slog.Info("offload", "", estimate)

	params := []string{
		"--model", modelPath,
		"--ctx-size", strconv.Itoa(opts.NumCtx),
		"--batch-size", strconv.Itoa(opts.NumBatch),
	}

	if opts.NumGPU >= 0 {
		params = append(params, "--n-gpu-layers", strconv.Itoa(opts.NumGPU))
	}

	if envconfig.Debug() {
		params = append(params, "--verbose")
	}

	if opts.MainGPU > 0 {
		params = append(params, "--main-gpu", strconv.Itoa(opts.MainGPU))
	}

	if len(adapters) > 0 {
		for _, adapter := range adapters {
			params = append(params, "--lora", adapter)
		}
	}

	defaultThreads := systemInfo.GetOptimalThreadCount()
	if opts.NumThread > 0 {
		params = append(params, "--threads", strconv.Itoa(opts.NumThread))
	} else if defaultThreads > 0 {
		params = append(params, "--threads", strconv.Itoa(defaultThreads))
	}

	fa := envconfig.FlashAttention()
	if fa && !gpus.FlashAttentionSupported() {
		//slog.Warn("flash attention enabled but not supported by gpu")
		fa = false
	}

	if fa && !f.SupportsFlashAttention() {
		//slog.Warn("flash attention enabled but not supported by model")
		fa = false
	}

	kvct := strings.ToLower(envconfig.KvCacheType())

	if fa {
		//slog.Info("enabling flash attention")
		params = append(params, "--flash-attn")

		// Flash Attention also supports kv cache quantization
		// Enable if the requested and kv cache type is supported by the model
		if kvct != "" && f.SupportsKVCacheType(kvct) {
			params = append(params, "--kv-cache-type", kvct)
		} else {
			//slog.Warn("kv cache type not supported by model", "type", kvct)
		}
	} else if kvct != "" && kvct != "f16" {
		//slog.Warn("quantized kv cache requested but flash attention disabled", "type", kvct)
	}

	// mmap has issues with partial offloading on metal
	for _, g := range gpus {
		if g.Library == "metal" &&
			uint64(opts.NumGPU) > 0 &&
			uint64(opts.NumGPU) < f.KV().BlockCount()+1 {
			opts.UseMMap = new(bool)
			*opts.UseMMap = false
		}
	}

	// Windows CUDA should not use mmap for best performance
	// Linux  with a model larger than free space, mmap leads to thrashing
	// For CPU loads we want the memory to be allocated, not FS cache
	if (runtime.GOOS == "windows" && gpus[0].Library == "cuda" && opts.UseMMap == nil) ||
		(runtime.GOOS == "linux" && systemFreeMemory < estimate.TotalSize && opts.UseMMap == nil) ||
		(gpus[0].Library == "cpu" && opts.UseMMap == nil) ||
		(opts.UseMMap != nil && !*opts.UseMMap) {
		params = append(params, "--no-mmap")
	}

	if opts.UseMLock {
		params = append(params, "--mlock")
	}

	// TODO - NUMA support currently doesn't work properly

	params = append(params, "--parallel", strconv.Itoa(numParallel))

	if estimate.TensorSplit != "" {
		params = append(params, "--tensor-split", estimate.TensorSplit)
	}

	if envconfig.MultiUserCache() {
		params = append(params, "--multiuser-cache")
	}

	libs := make(map[string]string)
	if entries, err := os.ReadDir(discover.LibOllamaPath); err == nil {
		for _, entry := range entries {
			libs[entry.Name()] = filepath.Join(discover.LibOllamaPath, entry.Name())
		}
	}

	lib := gpus[0].RunnerName()
	requested := envconfig.LLMLibrary()
	if libs[requested] != "" {
		//slog.Info("using requested gpu library", "requested", requested)
		lib = requested
	}

	var compatible []string
	for k := range libs {
		// exact match first
		if k == lib {
			compatible = append([]string{k}, compatible...)
			continue
		}

		// then match the family (e.g. 'cuda')
		if strings.Split(k, "_")[0] == strings.Split(lib, "_")[0] {
			compatible = append(compatible, k)
		}
	}
	//slog.Debug("compatible gpu libraries", "compatible", compatible)
	exe, err := os.Executable()
	if err != nil {
		return nil //, fmt.Errorf("unable to lookup executable path: %w", err)
	}

	if eval, err := filepath.EvalSymlinks(exe); err == nil {
		exe = eval
	}

	var llamaModel *llama.Model
	var textProcessor model.TextProcessor
	if envconfig.NewEngine() || f.KV().OllamaEngineRequired() {
		textProcessor, err = model.NewTextProcessor(modelPath)
		if err != nil {
			// To prepare for opt-out mode, instead of treating this as an error, we fallback to the old runner
			//slog.Debug("model not yet supported by Ollama engine, switching to compatibility mode", "model", modelPath, "error", err)
		}
	}
	if textProcessor == nil {
		llamaModel, err = llama.LoadModelFromFile(modelPath, llama.ModelParams{VocabOnly: true})
		if err != nil {
			return nil //, err
		}
	}

	if len(projectors) > 0 && llamaModel != nil {
		params = append(params, "--mmproj", projectors[0])
	}

	finalParams := []string{"runner"}
	if textProcessor != nil {
		// New engine
		// TODO - if we have failure to load scenarios, add logic to retry with the old runner
		finalParams = append(finalParams, "--ollama-engine")
	}
	finalParams = append(finalParams, params...)

	return finalParams
}
