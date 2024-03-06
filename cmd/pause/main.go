package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/carv-ics-forth/hpk/compute/endpoint"
	"github.com/carv-ics-forth/hpk/compute/image"
	"github.com/carv-ics-forth/hpk/compute/podhandler"
	"github.com/rs/zerolog/log"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getPodDetails(clientset *kubernetes.Clientset, namespace string, podID string) (*v1.Pod, error) {
	// Create a context with a 5-minute timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podID, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return pod, nil
}

func fileExists(filename string) bool {
	if filename == "" {
		return false
	}
	info, err := os.Stat(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		panic(err)
	}
	return !info.IsDir() // Ensure it's a file, not a directory
}

func main() {
	var podID string
	var namespaceID string
	var wg sync.WaitGroup

	flag.StringVar(&podID, "pod", "", "Pod ID to query Kubernetes")
	flag.StringVar(&namespaceID, "namespace", "", "Pod ID to query Kubernetes")
	flag.Parse()

	if podID == "" || namespaceID == "" {
		log.Fatal().Msg("Please provide both the pod and namespace.")
	}

	config, err := clientcmd.BuildConfigFromFlags("", filepath.Join("/k8s-data", "admin.conf"))
	if err != nil {
		log.Fatal().Err(err).Msg("Error building kubeconfig")
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal().Err(err).Msg("Error creating Kubernetes client")
	}

	// Main loop to keep asking for pod details
	timeout := time.After(5 * time.Minute)

acquire_pod_loop:
	for {
		select {
		case <-timeout:
			log.Error().Msg("Timeout reached. Exiting.")
			os.Exit(1)
		default:
			_, err := getPodDetails(clientset, namespaceID, podID)
			if err != nil {
				log.Error().Err(err).Msg("Error getting pod details. Retrying...")
				time.Sleep(5 * time.Second) // Adjust retry interval as needed
				continue
			}

			break acquire_pod_loop
		}
	}

	pod, err := getPodDetails(clientset, namespaceID, podID)
	if err != nil {
		panic(err)
	}

	if err := prepareContainers(pod); err != nil {
		log.Error().Err(err).Msg("Error preparing container environment")
		return
	}

	if len(pod.Spec.InitContainers) > 0 {
		if err := handleInitContainers(pod, true); err != nil {
			log.Error().Err(err).Msg("Error executing init containers")
			return
		}
	}

	if err := handleContainers(pod, &wg, true); err != nil {
		log.Error().Err(err).Msg("Error executing main containers")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGCHLD)

	go func() {
		for {
			select {
			case signo := <-signalChan:
				switch signo {
				case syscall.SIGINT, syscall.SIGTERM:
					log.Info().Msgf("Received %v. Cleaning up...\n", signo)
					cancel() // Initiate cleanup

					// Wait for containers to exit before fully exiting the goroutine
					wg.Wait()

				case syscall.SIGCHLD:
					// SIGCHLD handling - reap zombie processes
					log.Info().Msg("Received SIGCHLD. Containers have terminated. ")
					for {
						pid, err := syscall.Wait4(-1, nil, syscall.WNOHANG, nil)
						if pid <= 0 || err != nil {
							break
						}
						log.Info().Msgf("pid: %v", pid)
					}
					cancel() // Initiate cleanup after SIGCHLD handling

					// Wait for containers to exit before fully exiting the goroutine
					wg.Wait()
				}
			case <-ctx.Done():
				log.Info().Msg("Context was cancelled. Waiting for containers to exit...")
				wg.Wait() // Ensure completion of all containers
				log.Info().Msg("Containers have terminated. Exiting...")
				return // Terminate the goroutine once containers finish
			}

		}
	}()

	log.Info().Msg("Containers have started. Now waiting on context or signals")
	<-ctx.Done()

}

func prepareContainers(pod *v1.Pod) error {
	if err := prepareDNS(pod); err != nil {
		return fmt.Errorf("could not prepare DNS : %v", err)
	}
	if err := announceIP(pod); err != nil {
		return fmt.Errorf("could not announce ip : %v", err)
	}
	if err := cleanEnvironment(); err != nil {
		return fmt.Errorf("could not clear the environment : %v", err)
	}
	return nil
}

func announceIP(pod *v1.Pod) error {
	podKey := client.ObjectKeyFromObject(pod)
	hpk := endpoint.HPK(pod.Annotations["workingDirectory"])
	podPath := hpk.Pod(podKey)

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return fmt.Errorf("could not get interfaces from host: %v", err)
	}
	var ipAddresses []string
	for _, addr := range addrs {
		// Add only if the address is an IP address
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			ipAddresses = append(ipAddresses, ipNet.IP.String())
		}
	}
	ipString := strings.Join(ipAddresses, " ")

	if err := os.WriteFile(podPath.IPAddressPath(), []byte(ipString), os.ModePerm); err != nil {
		return fmt.Errorf("error writing to .ip file: %v", err)
	}
	return nil
}

func cleanEnvironment() error {

	envVars := []string{
		"LD_LIBRARY_PATH",
		"SINGULARITY_COMMAND",
		"SINGULARITY_CONTAINER",
		"SINGULARITY_ENVIRONMENT",
		"SINGULARITY_NAME",
		"APPTAINER_APPNAME",
		"APPTAINER_COMMAND",
		"APPTAINER_CONTAINER",
		"APPTAINER_ENVIRONMENT",
		"APPTAINER_NAME",
		"APPTAINER_BIND",
		"SINGULARITY_BIND",
	}

	for _, name := range envVars {
		if err := os.Unsetenv(name); err != nil {
			return fmt.Errorf("could not clear the environment variable %s: %v", name, err)
		}
	}
	return nil
}

func prepareDNS(pod *v1.Pod) error {
	if err := os.MkdirAll("/scratch/etc", 0644); err != nil {
		return fmt.Errorf("could not create /scratch/etc folder: %v", err)
	}

	kubeDNSIP := os.Getenv("KUBEDNS_IP")
	if kubeDNSIP == "" {
		return fmt.Errorf("KUBEDNS_IP environment variable not set")
	}

	// Create and write to /scratch/etc/resolv.conf
	resolvConfContent := fmt.Sprintf(`search %s.svc.cluster.local svc.cluster.local cluster.local
	nameserver %s
	options ndots:5`, pod.Namespace, kubeDNSIP)

	if err := os.WriteFile("/scratch/etc/resolv.conf", []byte(resolvConfContent), os.ModePerm); err != nil {
		return fmt.Errorf("error writing to resolv.conf: %v", err)
	}

	// Add hostname to /scratch/etc/hosts
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("error getting hostname: %v", err)
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return fmt.Errorf("could not get interfaces from host: %v", err)
	}

	var ipAddresses []string
	for _, addr := range addrs {
		// Add only if the address is an IP address
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			ipAddresses = append(ipAddresses, ipNet.IP.String())
		}
	}
	ipString := strings.Join(ipAddresses, " ") + " " + hostname

	hostsContent := fmt.Sprintf("127.0.0.1 localhost\n%s \n", ipString)

	if err := os.WriteFile("/scratch/etc/hosts", []byte(hostsContent), os.ModePerm); err != nil {
		return fmt.Errorf("error writing to hosts: %v", err)
	}
	DebugDNSInfo(resolvConfContent, hostsContent)
	return nil
}

func DebugDNSInfo(resolvConfContent string, hostsContent string) {
	fmt.Printf("====================================================================\n%s\n", resolvConfContent)
	fmt.Printf("====================================================================\n%s", hostsContent)
	fmt.Println("====================================================================")

}

func handleInitContainers(pod *v1.Pod, hpkEnv bool) error {
	isDebug := os.Getenv("DEBUG_MODE") == "true"
	podKey := client.ObjectKeyFromObject(pod)
	hpk := endpoint.HPK(pod.Annotations["workingDirectory"])
	podPath := hpk.Pod(podKey)
	for _, container := range pod.Spec.InitContainers {
		effectiSecurityContext := podhandler.DetermineEffectiveSecurityContext(pod, &container)
		uid, gid := podhandler.DetermineEffectiveRunAsUser(effectiSecurityContext)
		log.Info().Msgf("Spawning init container: %s", container.Name)
		instanceName := fmt.Sprintf("%s_%s_%s", pod.GetNamespace(), pod.GetName(), container.Name)

		containerPath := podPath.Container(container.Name)
		envFilePath := containerPath.EnvFilePath()

		// Environment File Handling
		if fileExists(envFilePath) {
			output, err := exec.Command("sh", "-c", envFilePath).CombinedOutput()
			if err != nil {
				return fmt.Errorf("error executing EnvFilePath: %v, output: %s", err, output)
			}
			envFileName := filepath.Join("/scratch", instanceName+".env")
			if err := os.WriteFile(envFileName, output, 0644); err != nil {
				return fmt.Errorf("error writing env file: %v", err)
			}
		}

		executionMode := "exec"
		if container.Command == nil {
			executionMode = "run"
		}

		binds := make([]string, len(container.VolumeMounts))

		// check the code from https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/kubelet_pods.go#L196
		for i, mount := range container.VolumeMounts {
			hostPath := filepath.Join(podPath.VolumeDir(), mount.Name)

			// subPath := mount.SubPath
			// if mount.SubPathExpr != "" {
			// 	subPath, err = helpers.ExpandContainerVolumeMounts(mount, h.podEnvVariables)
			// 	if err != nil {
			// 		compute.SystemPanic(err, "cannot expand env variables for container '%s' of pod '%s'", container, h.podKey)
			// 	}
			// }

			// if subPath != "" {
			// 	if filepath.IsAbs(subPath) {
			// 		return fmt.Errorf("error SubPath '%s' must not be an absolute path", subPath)
			// 	}

			// 	subPathFile := filepath.Join(hostPath, subPath)

			// 	// mount the subpath
			// 	hostPath = subPathFile
			// }

			accessMode := "rw"
			if mount.ReadOnly {
				accessMode = "ro"
			}

			binds[i] = hostPath + ":" + mount.MountPath + ":" + accessMode
		}

		// Apptainer Command Construction
		apptainerVerbosity := "--quiet"
		if isDebug {
			apptainerVerbosity = "--debug"
		}
		apptainerArgs := []string{
			apptainerVerbosity, executionMode, "--cleanenv", "--writable-tmpfs", "--no-mount", "home", "--unsquash",
		}
		if hpkEnv {
			apptainerArgs = append(apptainerArgs, "--bind", "/scratch/etc/resolv.conf:/etc/resolv.conf,/scratch/etc/hosts:/etc/hosts")
		}
		if len(binds) > 0 {
			bindArgs := &apptainerArgs[len(apptainerArgs)-1]
			*bindArgs += "," + strings.Join(binds, ",")
		}
		if uid != 0 {
			apptainerArgs = append(apptainerArgs, "--security", fmt.Sprintf("uid:%d,gid:%d", uid, uid), "--userns")
		}
		if gid != 0 {
			apptainerArgs = append(apptainerArgs, "--security", fmt.Sprintf("gid:%d", gid), "--userns")
		}

		if fileExists(envFilePath) {
			apptainerArgs = append(apptainerArgs, "--env-file", filepath.Join("/scratch", instanceName+".env"))
		}

		apptainerArgs = append(apptainerArgs, hpk.ImageDir()+image.ParseImageName(container.Image))
		apptainerArgs = append(apptainerArgs, container.Command...)
		apptainerArgs = append(apptainerArgs, container.Args...)

		// Get the PID
		pid := os.Getpid()
		if err := os.WriteFile(containerPath.IDPath(), []byte(fmt.Sprintf("pid://%d", pid)), 0644); err != nil {
			return fmt.Errorf("failed to create pid file") // Log the error
		}

		// Execute Apptainer (Blocking)
		log.Debug().Msg(fmt.Sprintf("ApptainerArgs: %v", apptainerArgs))
		cmd := exec.Command("apptainer", apptainerArgs...)
		cmd.Env = os.Environ()

		// Open log file
		logFile, err := os.Create(containerPath.LogsPath())
		if err != nil {
			return fmt.Errorf("failed to create log file: %v", err)
		}
		defer logFile.Close()

		// // Redirect output to log file
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		if err := cmd.Run(); err != nil {
			log.Error().Err(err).Msgf("Error executing init container: %s", container.Name)
			return fmt.Errorf("init container failed: %v", err) // Abort on failure
		}
		if err := os.WriteFile(containerPath.ExitCodePath(), []byte(strconv.Itoa(cmd.ProcessState.ExitCode())), 0644); err != nil {
			return fmt.Errorf("failed to create exitCode file") // Log the error
		}
	}
	return nil
}

func handleContainers(pod *v1.Pod, wg *sync.WaitGroup, hpkEnv bool) error {
	isDebug := os.Getenv("DEBUG_MODE") == "true"
	podKey := client.ObjectKeyFromObject(pod)
	hpk := endpoint.HPK(pod.Annotations["workingDirectory"])
	podPath := hpk.Pod(podKey)
	for _, container := range pod.Spec.Containers {
		effectiSecurityContext := podhandler.DetermineEffectiveSecurityContext(pod, &container)
		uid, gid := podhandler.DetermineEffectiveRunAsUser(effectiSecurityContext)
		instanceName := fmt.Sprintf("%s_%s_%s", pod.GetNamespace(), pod.GetName(), container.Name)

		containerPath := podPath.Container(container.Name)
		envFilePath := containerPath.EnvFilePath()

		// Environment File Handling
		if fileExists(envFilePath) {
			output, err := exec.Command("sh", "-c", envFilePath).CombinedOutput()
			if err != nil {
				return fmt.Errorf("error executing EnvFilePath: %v, output: %s", err, output)
			}
			envFileName := filepath.Join("/scratch", instanceName+".env")
			if err := os.WriteFile(envFileName, output, 0644); err != nil {
				return fmt.Errorf("error writing env file: %v", err)
			}
		}

		executionMode := "exec"
		if container.Command == nil {
			executionMode = "run"
		}

		binds := make([]string, len(container.VolumeMounts))

		// check the code from https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/kubelet_pods.go#L196
		for i, mount := range container.VolumeMounts {
			hostPath := filepath.Join(podPath.VolumeDir(), mount.Name)

			// subPath := mount.SubPath
			// if mount.SubPathExpr != "" {
			// 	subPath, err = helpers.ExpandContainerVolumeMounts(mount, h.podEnvVariables)
			// 	if err != nil {
			// 		compute.SystemPanic(err, "cannot expand env variables for container '%s' of pod '%s'", container, h.podKey)
			// 	}
			// }

			// if subPath != "" {
			// 	if filepath.IsAbs(subPath) {
			// 		return fmt.Errorf("error SubPath '%s' must not be an absolute path", subPath)
			// 	}

			// 	subPathFile := filepath.Join(hostPath, subPath)

			// 	// mount the subpath
			// 	hostPath = subPathFile
			// }

			accessMode := "rw"
			if mount.ReadOnly {
				accessMode = "ro"
			}

			binds[i] = hostPath + ":" + mount.MountPath + ":" + accessMode
		}

		// Apptainer Command Construction
		apptainerVerbosity := "--quiet"
		if isDebug {
			apptainerVerbosity = "--debug"
		}
		apptainerArgs := []string{
			apptainerVerbosity, executionMode, "--cleanenv", "--writable-tmpfs", "--no-mount", "home", "--unsquash",
		}
		if hpkEnv {
			apptainerArgs = append(apptainerArgs, "--bind", "/scratch/etc/resolv.conf:/etc/resolv.conf,/scratch/etc/hosts:/etc/hosts")
			if len(binds) > 0 {
				bindArgs := &apptainerArgs[len(apptainerArgs)-1]
				*bindArgs += "," + strings.Join(binds, ",")
			}
		}
		if uid != 0 {
			apptainerArgs = append(apptainerArgs, "--security", fmt.Sprintf("uid:%d,gid:%d", uid, uid), "--userns")
		}
		if gid != 0 {
			apptainerArgs = append(apptainerArgs, "--security", fmt.Sprintf("gid:%d", gid), "--userns")
		}

		if fileExists(envFilePath) {
			apptainerArgs = append(apptainerArgs, "--env-file", filepath.Join("/scratch", instanceName+".env"))
		}

		apptainerArgs = append(apptainerArgs, hpk.ImageDir()+image.ParseImageName(container.Image))
		apptainerArgs = append(apptainerArgs, container.Command...)
		apptainerArgs = append(apptainerArgs, container.Args...)

		wg.Add(1)
		go func(container v1.Container) { // Ensure container cleanup
			defer wg.Done()
			// Execute Apptainer in Background
			log.Debug().Msg(fmt.Sprintf("ApptainerArgs: %v", apptainerArgs))
			cmd := exec.Command("apptainer", apptainerArgs...)
			cmd.Env = os.Environ()
			// If needed, get references to stdout and stderr
			// log.Debug().Msgf("LogPath: %s", containerPath.LogsPath())
			logFile, err := os.Create(containerPath.LogsPath())
			if err != nil {
				log.Error().Err(err).Msgf("Failed to create log file %s", containerPath.LogsPath())
				return
			}
			defer logFile.Close()

			cmd.Stdout = logFile
			cmd.Stderr = logFile
			log.Info().Msgf("Spawning main container: %s", container.Name)
			// Start the  container
			if err := cmd.Start(); err != nil {
				log.Error().Err(err).Msg("Failed to start Apptainer container")
			}

			// Get the PID
			pid := cmd.Process.Pid
			if err := os.WriteFile(containerPath.IDPath(), []byte(fmt.Sprintf("pid://%d", pid)), 0644); err != nil {
				log.Error().Err(err).Msg("Failed to create pid file") // Log the error
				return
			}

			// Handle Exit (consider moving output writing or using cmd.Wait)
			if err := cmd.Wait(); err != nil {
				log.Error().Err(err).Msgf("error executing container: %s, because of %v", container.Name, err)
			}

			if err := os.WriteFile(containerPath.ExitCodePath(), []byte(strconv.Itoa(cmd.ProcessState.ExitCode())), 0644); err != nil {
				log.Error().Err(err).Msg("Failed to create exitCode file") // Log the error
				return
			}

		}(container)

	}
	return nil
}

/**

if err := scriptTemplate.Execute(&scriptFileContent, JobFields{
		Pod:                h.podKey,
		PauseImageFilePath: pauseImage.Filepath,
		HostEnv:            compute.Environment,
		VirtualEnv: compute.VirtualEnvironment{
			PodDirectory:        h.podDirectory.String(),
			CgroupFilePath:      h.podDirectory.CgroupFilePath(),
			ConstructorFilePath: h.podDirectory.ConstructorFilePath(),
			IPAddressPath:       h.podDirectory.IPAddressPath(),
			StdoutPath:          h.podDirectory.StdoutPath(),
			StderrPath:          h.podDirectory.StderrPath(),
			SysErrorFilePath:    h.podDirectory.SysErrorFilePath(),
		},
		InitContainers:  initContainers,
		Containers:      containers,
		ResourceRequest: resources.ResourceListToStruct(resourceRequest),
		CustomFlags:     customFlags,
	}); err != nil {
		//-- since both the template and fields are internal to the code, the evaluation should always succeed	--
		compute.SystemPanic(err, "failed to evaluate sbatch template")
	}
**/

/**
#!/bin/bash

############################
# Auto-Generated Script    #
# Please do not edit. 	   #
############################

# If any command fails, the script will immediately exit,
# and unset variables or errors in pipelines are treated as errors

set -eum pipeline

function debug_info() {
	echo -e "\n"
	echo "=============================="
	echo " Compute Environment Info"
	echo "=============================="
	echo "* DNS: {{.HostEnv.KubeDNS}}"
	echo "* PodDir: {{.VirtualEnv.PodDirectory}}"
	echo "=============================="
	echo -e "\n"
	echo "=============================="
	echo " Virtual Environment Info"
	echo "=============================="
	echo "* Host: $(hostname)"
	echo "* IP: $(hostname -I)"
	echo "* User: $(id)"
	echo "=============================="
	echo -e "\n"
}

handle_dns() {
	mkdir -p /scratch/etc

# Rewire /scratch/etc/resolv.conf to point to KubeDNS
cat > /scratch/etc/resolv.conf << DNS_EOF
search {{.Pod.Namespace}}.svc.cluster.local svc.cluster.local cluster.local
nameserver {{.HostEnv.KubeDNS}}
options ndots:5
DNS_EOF

	# Add hostname to known hosts. Required for loopback
	echo -e "127.0.0.1 localhost" >> /scratch/etc/hosts
	echo -e "$(hostname -I) $(hostname)" >> /scratch/etc/hosts
}

# If not removed, Flags will be consumed by the nested Singularity and overwrite paths.
# https://docs.sylabs.io/guides/3.11/user-guide/environment_and_metadata.html
function reset_env() {
	unset LD_LIBRARY_PATH

	unset SINGULARITY_COMMAND
	unset SINGULARITY_CONTAINER
	unset SINGULARITY_ENVIRONMENT
	unset SINGULARITY_NAME

	unset APPTAINER_APPNAME
	unset APPTAINER_COMMAND
	unset APPTAINER_CONTAINER
	unset APPTAINER_ENVIRONMENT
	unset APPTAINER_NAME

	unset APPTAINER_BIND
	unset SINGULARITY_BIND
}

function cleanup() {
	lastCommand=$1
	exitCode=$2

	echo "[Virtual] Ensure all background jobs are terminated".
	wait

	if [[ $exitCode -eq 0 ]]; then
		echo "[Virtual] Gracefully exit the Virtual Environment. All resources will be released."
	else
		echo "[Virtual] **SYSTEMERROR** ${lastCommand} command filed with exit code ${exitCode}" | tee {{.VirtualEnv.SysErrorFilePath}}
	fi

	exit ${exitCode}
}

function handle_init_containers() {
{{range $index, $container := .InitContainers}}
	####################
	##  New Container  #
	####################

	echo "[Virtual] Spawning InitContainer: {{$container.InstanceName}}"

	{{- if $container.EnvFilePath}}
	sh -c {{$container.EnvFilePath}} > /scratch/{{$container.InstanceName}}.env
	{{- end}}

	# Mark the beginning of an init job (all get the shell's pid).
	echo pid://$$ > {{$container.JobIDPath}}


	$(apptainer {{ $container.ExecutionMode }} --cleanenv --writable-tmpfs --no-mount home --unsquash \
	{{- if $container.RunAsUser}}
	--security uid:{{$container.RunAsUser}},gid:{{$container.RunAsUser}} --userns \
	{{- end}}
	{{- if $container.RunAsGroup}}
	--security gid:{{$container.RunAsGroup}} --userns \
	{{- end}}
	--bind /scratch/etc/resolv.conf:/etc/resolv.conf,/scratch/etc/hosts:/etc/hosts,{{join "," $container.Binds}} \
	{{- if $container.EnvFilePath}}
	--env-file /scratch/{{$container.InstanceName}}.env \
	{{- end}}
	{{$container.ImageFilePath}}
	{{- if $container.Command}}
		{{- range $index, $cmd := $container.Command}} {{$cmd | param}} {{- end}}
	{{- end -}}
	{{- if $container.Args}}
		{{range $index, $arg := $container.Args}} {{$arg | param}} {{- end}}
	{{- end }} \
	&>> {{$container.LogsPath}})

	# Mark the ending of an init job.
	echo $? > {{$container.ExitCodePath}}
{{end}}

	echo "[Virtual] All InitContainers have been completed."
	return
}

function handle_containers() {
{{range $index, $container := .Containers}}
	####################
	##  New Container  #
	####################

	{{- if $container.EnvFilePath}}
	sh -c {{$container.EnvFilePath}} > /scratch/{{$container.InstanceName}}.env
	{{- end}}

	$(apptainer {{ $container.ExecutionMode }} --cleanenv --writable-tmpfs --no-mount home --unsquash \
	{{- if $container.RunAsUser}}
	--security uid:{{$container.RunAsUser}},gid:{{$container.RunAsUser}} --userns \
	{{- end}}
	{{- if $container.RunAsGroup}}
	--security gid:{{$container.RunAsGroup}} --userns \
	{{- end}}
	--bind /scratch/etc/resolv.conf:/etc/resolv.conf,/scratch/etc/hosts:/etc/hosts,{{join "," $container.Binds}} \
	{{- if $container.EnvFilePath}}
	--env-file /scratch/{{$container.InstanceName}}.env \
	{{- end}}
	{{$container.ImageFilePath}}
	{{- if $container.Command}}
		{{- range $index, $cmd := $container.Command}} {{$cmd | param}} {{- end}}
	{{- end -}}
	{{- if $container.Args}}
		{{- range $index, $arg := $container.Args}} {{$arg | param}} {{- end}}
	{{- end }} \
	&>> {{$container.LogsPath}}; \
	echo $? > {{$container.ExitCodePath}}) &

	pid=$!
	echo pid://${pid} > {{$container.JobIDPath}}
	echo "[Virtual] Container started: {{$container.InstanceName}} ${pid}"
{{end}}

	######################
	##  Wait Containers  #
	######################

	echo "[Virtual] ... Waiting for containers to complete ..."
	wait  || echo "[Virtual] ... wait failed with error: $?"
	echo "[Virtual] ... Containers terminated ..."
}



debug_info

echo "[Virtual] Resetting Environment ..."
reset_env

echo "[Virtual] Announcing IP ..."
echo $(hostname -I) > {{.VirtualEnv.IPAddressPath}}

echo "[Virtual] Setting DNS ..."
handle_dns

echo "[Virtual] Setting Cleanup Handler ..."
trap 'cleanup "${BASH_COMMAND}" "$?"'  EXIT

{{if gt (len .InitContainers) 0 }} handle_init_containers {{end}}

{{if gt (len .Containers) 0 }} handle_containers {{end}}
**/
