package exec

import (
	"context"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/zdnscloud/gok8s/cache"
	"github.com/zdnscloud/gok8s/client"
	"github.com/zdnscloud/gok8s/controller"
	"github.com/zdnscloud/gok8s/predicate"
)

type Executor struct {
	k8sCfg     *rest.Config
	client     client.Client
	podWatcher *podWatcher
	stopCh     chan struct{}
}

func New(k8sCfg *rest.Config) (*Executor, error) {
	cli, err := client.New(k8sCfg, client.Options{})
	if err != nil {
		return nil, err
	}

	cache, err := cache.New(k8sCfg, cache.Options{})
	if err != nil {
		return nil, err
	}

	stopCh := make(chan struct{})
	go cache.Start(stopCh)
	cache.WaitForCacheSync(stopCh)

	ctrl := controller.New("podWatcher", cache, scheme.Scheme)
	ctrl.Watch(&corev1.Pod{})
	podWatcher := newPodWatcher()
	go ctrl.Start(stopCh, podWatcher, predicate.NewIgnoreUnchangedUpdate())

	return &Executor{
		k8sCfg:     k8sCfg,
		client:     cli,
		podWatcher: podWatcher,
		stopCh:     stopCh,
	}, nil
}

type Pod struct {
	Namespace string
	Name      string
	Image     string
}

type Cmd struct {
	Path   string
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func (e *Executor) RunCmd(p Pod, c Cmd) error {
	pod, err := e.createPod(p, c)
	if err != nil {
		return err
	}
	ready := e.podWatcher.AddNotifyTask(p.Namespace, p.Name)
	<-ready
	err = e.attachPod(p, c)
	e.client.Delete(context.TODO(), pod)
	<-time.After(10 * time.Second)
	return err
}

func (e *Executor) createPod(p Pod, c Cmd) (*corev1.Pod, error) {
	privileged := false
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.Name,
			Namespace: p.Namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					TTY:     false,
					Stdin:   true,
					Name:    p.Name,
					Image:   p.Image,
					Command: []string{c.Path},
					Args:    c.Args,
					SecurityContext: &corev1.SecurityContext{
						Privileged: &privileged,
					},
					ImagePullPolicy: corev1.PullPolicy(corev1.PullAlways),
				},
			},
			RestartPolicy: corev1.RestartPolicyOnFailure,
		},
	}
	return pod, e.client.Create(context.TODO(), pod)
}

func (e *Executor) attachPod(p Pod, c Cmd) error {
	clientset, err := kubernetes.NewForConfig(e.k8sCfg)
	if err != nil {
		return err
	}
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(p.Name).
		Namespace(p.Namespace).
		SubResource("attach")
	opts := &corev1.PodAttachOptions{
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
		Container: p.Name,
	}
	req.VersionedParams(opts, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(e.k8sCfg, "POST", req.URL())
	if err != nil {
		return err
	}
	return exec.Stream(remotecommand.StreamOptions{
		Stdin:  c.Stdin,
		Stdout: c.Stdout,
		Stderr: c.Stderr,
	})
}
