package utils

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// This code is for big parts from https://github.com/openshift-kni/performance-addon-operators/tree/master/functests/utils

// GetBootTime gets the boot time of the given node by running a pod on it executing uptime command
func GetBootTime(c *kubernetes.Clientset, nodeName string, log logr.Logger) (*time.Time, error) {

	// create a pod and wait that it's running
	pod := getBootTimePod(nodeName)
	pod, err := c.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	err = waitForCondition(c, pod, corev1.PodReady, corev1.ConditionTrue, time.Minute)
	if err != nil {
		return nil, err
	}

	log.Info("boot time pod is running, going to execute uptime command")

	// get boot time
	// ubi does noy have uptime command, so we need to install it...
	bootTimeCommand := []string{"sh", "-c", "microdnf install procps -y >/dev/null 2>&1 && uptime -s"}
	bytes, err := waitForPodOutput(c, pod, bootTimeCommand)
	if err != nil {
		return nil, err
	}
	bootTime, err := time.Parse("2006-01-02 15:04:05", strings.TrimSpace(string(bytes)))
	if err != nil {
		return nil, err
	}

	return &bootTime, nil
}

func waitForPodOutput(c *kubernetes.Clientset, pod *corev1.Pod, command []string) ([]byte, error) {
	var out []byte
	if err := wait.PollImmediate(15*time.Second, time.Minute, func() (done bool, err error) {
		out, err = execCommandOnPod(c, pod, command)
		if err != nil {
			return false, err
		}

		return len(out) != 0, nil
	}); err != nil {
		return nil, err
	}

	return out, nil
}

// execCommandOnPod runs command in the pod and returns buffer output
func execCommandOnPod(c *kubernetes.Clientset, pod *corev1.Pod, command []string) ([]byte, error) {
	var outputBuf bytes.Buffer
	var errorBuf bytes.Buffer

	req := c.CoreV1().RESTClient().
		Post().
		Namespace(pod.Namespace).
		Resource("pods").
		Name(pod.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: pod.Spec.Containers[0].Name,
			Command:   command,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	exec, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return nil, err
	}

	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  os.Stdin,
		Stdout: &outputBuf,
		Stderr: &errorBuf,
		Tty:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to run command %v: output %s; error %s", command, outputBuf.String(), errorBuf.String())
	}

	if errorBuf.Len() != 0 {
		return nil, fmt.Errorf("failed to run command %v: output %s; error %s", command, outputBuf.String(), errorBuf.String())
	}

	return outputBuf.Bytes(), nil
}

// waitForCondition waits until the pod will have specified condition type with the expected status
func waitForCondition(c *kubernetes.Clientset, pod *corev1.Pod, conditionType corev1.PodConditionType, conditionStatus corev1.ConditionStatus, timeout time.Duration) error {
	return wait.PollImmediate(time.Second, timeout, func() (bool, error) {
		updatedPod := &corev1.Pod{}
		var err error
		if updatedPod, err = c.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{}); err != nil {
			return false, nil
		}
		for _, c := range updatedPod.Status.Conditions {
			if c.Type == conditionType && c.Status == conditionStatus {
				return true, nil
			}
		}
		return false, nil
	})
}

func getBootTimePod(nodeName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "boot-time-",
			Labels: map[string]string{
				"test": "",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{
				{
					Name:    "test",
					Image:   "registry.access.redhat.com/ubi8/ubi-minimal",
					Command: []string{"sleep", "2m"},
				},
			},
		},
	}
}
